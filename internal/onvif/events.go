package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	createPullPointAction = "http://www.onvif.org/ver10/events/wsdl/EventPortType/CreatePullPointSubscriptionRequest"
	pullMessagesAction    = "http://www.onvif.org/ver10/events/wsdl/PullPointSubscription/PullMessagesRequest"
	unsubscribeAction     = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest"
)

// PullPointSubscription contains the endpoint and reference parameters returned
// by an ONVIF event service. ReferenceParameters must be copied verbatim to
// subsequent PullMessages and Unsubscribe requests.
type PullPointSubscription struct {
	Address             string
	ReferenceParameters string
	CurrentTime         time.Time
	TerminationTime     time.Time
}

// EventNotification is a protocol-neutral representation of one native ONVIF
// notification. Items contains the SimpleItem name/value pairs supplied by the
// camera.
type EventNotification struct {
	Topic             string
	UTCTime           time.Time
	PropertyOperation string
	Items             map[string]string
}

func (c *Client) CreatePullPointSubscription(ctx context.Context, eventEndpoint string, lifetime time.Duration) (PullPointSubscription, error) {
	if strings.TrimSpace(eventEndpoint) == "" {
		return PullPointSubscription{}, errors.New("endpoint ONVIF de eventos vacío")
	}
	if lifetime < time.Minute {
		lifetime = 5 * time.Minute
	}
	body := `<tev:CreatePullPointSubscription xmlns:tev="http://www.onvif.org/ver10/events/wsdl"><tev:InitialTerminationTime>` +
		xmlEscape(xmlDuration(lifetime)) + `</tev:InitialTerminationTime></tev:CreatePullPointSubscription>`
	header := addressingHeader(createPullPointAction, eventEndpoint, "")
	raw, err := c.soapWithHeader(ctx, eventEndpoint, createPullPointAction, body, header)
	if err != nil {
		return PullPointSubscription{}, fmt.Errorf("crear suscripción ONVIF: %w", err)
	}

	var envelope struct {
		Body struct {
			Response struct {
				SubscriptionReference struct {
					Address string `xml:"Address"`
				} `xml:"SubscriptionReference"`
				CurrentTime     string `xml:"CurrentTime"`
				TerminationTime string `xml:"TerminationTime"`
			} `xml:"CreatePullPointSubscriptionResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &envelope); err != nil {
		return PullPointSubscription{}, fmt.Errorf("decodificar suscripción ONVIF: %w", err)
	}
	response := envelope.Body.Response
	address := strings.TrimSpace(response.SubscriptionReference.Address)
	if address == "" {
		return PullPointSubscription{}, errors.New("la cámara no devolvió la dirección PullPoint")
	}
	normalized, err := normalizeEndpointHost(address, eventEndpoint)
	if err != nil {
		return PullPointSubscription{}, fmt.Errorf("dirección PullPoint inválida: %w", err)
	}
	referenceParameters, err := extractReferenceParameters(raw)
	if err != nil {
		return PullPointSubscription{}, fmt.Errorf("decodificar parámetros de referencia ONVIF: %w", err)
	}
	return PullPointSubscription{
		Address:             normalized,
		ReferenceParameters: referenceParameters,
		CurrentTime:         parseONVIFTime(response.CurrentTime),
		TerminationTime:     parseONVIFTime(response.TerminationTime),
	}, nil
}

func (c *Client) PullMessages(ctx context.Context, subscription PullPointSubscription, wait time.Duration, limit int) ([]EventNotification, time.Time, error) {
	if strings.TrimSpace(subscription.Address) == "" {
		return nil, time.Time{}, errors.New("suscripción ONVIF sin dirección")
	}
	if wait < time.Second {
		wait = 20 * time.Second
	}
	if wait > time.Minute {
		wait = time.Minute
	}
	if limit < 1 {
		limit = 64
	}
	if limit > 256 {
		limit = 256
	}
	body := `<tev:PullMessages xmlns:tev="http://www.onvif.org/ver10/events/wsdl"><tev:Timeout>` +
		xmlEscape(xmlDuration(wait)) + `</tev:Timeout><tev:MessageLimit>` + strconv.Itoa(limit) + `</tev:MessageLimit></tev:PullMessages>`
	header := addressingHeader(pullMessagesAction, subscription.Address, subscription.ReferenceParameters)
	raw, err := c.soapWithHeader(ctx, subscription.Address, pullMessagesAction, body, header)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("recibir eventos ONVIF: %w", err)
	}
	notifications, termination, err := parsePullMessages(raw)
	if err != nil {
		return nil, time.Time{}, err
	}
	return notifications, termination, nil
}

func (c *Client) Unsubscribe(ctx context.Context, subscription PullPointSubscription) error {
	if strings.TrimSpace(subscription.Address) == "" {
		return nil
	}
	body := `<wsnt:Unsubscribe xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"/>`
	header := addressingHeader(unsubscribeAction, subscription.Address, subscription.ReferenceParameters)
	if _, err := c.soapWithHeader(ctx, subscription.Address, unsubscribeAction, body, header); err != nil {
		return fmt.Errorf("cancelar suscripción ONVIF: %w", err)
	}
	return nil
}

// ProbeEventSubscription verifies that the camera supports the native ONVIF
// PullPoint mechanism. It creates and immediately removes a short subscription.
func (c *Client) ProbeEventSubscription(ctx context.Context, host string) error {
	endpoint, err := c.DiscoverEventService(ctx, host)
	if err != nil {
		return fmt.Errorf("descubrir eventos ONVIF: %w", err)
	}
	subscription, err := c.CreatePullPointSubscription(ctx, endpoint, 2*time.Minute)
	if err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Unsubscribe(cleanupCtx, subscription)
	return nil
}

// extractReferenceParameters re-encodes the returned XML tokens instead of
// copying raw inner XML. Cameras often declare vendor namespaces on an outer
// SOAP element; re-encoding preserves those namespace URIs in a standalone
// fragment and prevents malformed or injectable headers.
func extractReferenceParameters(raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "ReferenceParameters" {
			continue
		}

		var buffer bytes.Buffer
		encoder := xml.NewEncoder(&buffer)
		depth := 1
		for depth > 0 {
			token, err = decoder.Token()
			if err != nil {
				return "", err
			}
			switch token.(type) {
			case xml.StartElement:
				depth++
				if err := encoder.EncodeToken(token); err != nil {
					return "", err
				}
			case xml.EndElement:
				depth--
				if depth > 0 {
					if err := encoder.EncodeToken(token); err != nil {
						return "", err
					}
				}
			default:
				if depth > 0 {
					if err := encoder.EncodeToken(token); err != nil {
						return "", err
					}
				}
			}
		}
		if err := encoder.Flush(); err != nil {
			return "", err
		}
		return strings.TrimSpace(buffer.String()), nil
	}
}

func parsePullMessages(raw []byte) ([]EventNotification, time.Time, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var notifications []EventNotification
	var termination time.Time
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("decodificar eventos ONVIF: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "TerminationTime":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nil, time.Time{}, err
			}
			if parsed := parseONVIFTime(value); !parsed.IsZero() {
				termination = parsed
			}
		case "NotificationMessage":
			notification, err := decodeNotification(decoder, start)
			if err != nil {
				return nil, time.Time{}, err
			}
			notifications = append(notifications, notification)
		}
	}
	return notifications, termination, nil
}

func decodeNotification(decoder *xml.Decoder, root xml.StartElement) (EventNotification, error) {
	notification := EventNotification{Items: make(map[string]string)}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return EventNotification{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			switch value.Name.Local {
			case "Topic":
				var topic string
				if err := decoder.DecodeElement(&topic, &value); err != nil {
					return EventNotification{}, err
				}
				depth--
				notification.Topic = strings.TrimSpace(topic)
			case "Message":
				for _, attribute := range value.Attr {
					switch attribute.Name.Local {
					case "UtcTime":
						notification.UTCTime = parseONVIFTime(attribute.Value)
					case "PropertyOperation":
						notification.PropertyOperation = strings.TrimSpace(attribute.Value)
					}
				}
			case "SimpleItem":
				name, itemValue := "", ""
				for _, attribute := range value.Attr {
					switch attribute.Name.Local {
					case "Name":
						name = strings.TrimSpace(attribute.Value)
					case "Value":
						itemValue = strings.TrimSpace(attribute.Value)
					}
				}
				if name != "" {
					notification.Items[name] = itemValue
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return notification, nil
}

// ClassifyEvent maps common native ONVIF topics to the event types already
// understood by Fragata. It returns recognized=false for initialization and
// explicit inactive transitions.
func ClassifyEvent(notification EventNotification) (eventType string, active bool, recognized bool, key string) {
	operation := strings.ToLower(strings.TrimSpace(notification.PropertyOperation))
	if operation == "initialized" || operation == "initialize" {
		return "", false, false, notificationKey(notification)
	}

	combinedParts := []string{notification.Topic}
	stateFound := false
	activeState := false
	inactiveState := false
	for name, value := range notification.Items {
		combinedParts = append(combinedParts, name, value)
		if !looksLikeStateName(name) {
			continue
		}
		if state, known := parseEventState(value); known {
			stateFound = true
			if state {
				activeState = true
			} else {
				inactiveState = true
			}
		}
	}
	if stateFound && !activeState && inactiveState {
		return "", false, false, notificationKey(notification)
	}

	combined := strings.ToLower(strings.Join(combinedParts, " "))
	switch {
	case containsAny(combined, "person", "human", "people", "pedestrian", "face", "body"):
		eventType = "person"
	case containsAny(combined, "motion", "cellmotion", "fielddetector", "moving", "movement"):
		eventType = "motion"
	case containsAny(combined, "alarm", "analytics", "linecross", "crossed", "region", "loiter", "tamper", "intrusion", "digitalinput", "trigger"):
		eventType = "onvif"
	default:
		// Native cameras use vendor-specific topic names. A non-empty topic is
		// still a valid event as long as it is not an inactive property update.
		if strings.TrimSpace(notification.Topic) == "" {
			return "", false, false, notificationKey(notification)
		}
		eventType = "onvif"
	}
	return eventType, true, true, notificationKey(notification)
}

func notificationKey(notification EventNotification) string {
	parts := []string{strings.ToLower(strings.TrimSpace(notification.Topic))}
	keys := make([]string, 0, len(notification.Items))
	for name := range notification.Items {
		lower := strings.ToLower(name)
		if looksLikeStateName(name) || strings.Contains(lower, "time") || strings.Contains(lower, "date") {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		parts = append(parts, strings.ToLower(name)+"="+strings.ToLower(notification.Items[name]))
	}
	return strings.Join(parts, "|")
}

func looksLikeStateName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return containsAny(name, "state", "active", "motion", "alarm", "status", "logical", "trigger", "detected", "value")
}

func parseEventState(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "active", "detected", "alarm", "start", "started", "yes", "high":
		return true, true
	case "0", "false", "off", "inactive", "idle", "clear", "cleared", "stop", "stopped", "no", "low":
		return false, true
	default:
		return false, false
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func addressingHeader(action, target, referenceParameters string) string {
	messageID := "urn:uuid:" + randomHexID()
	header := `<wsa:Action xmlns:wsa="http://www.w3.org/2005/08/addressing" s:mustUnderstand="1">` + xmlEscape(action) + `</wsa:Action>` +
		`<wsa:MessageID xmlns:wsa="http://www.w3.org/2005/08/addressing">` + messageID + `</wsa:MessageID>` +
		`<wsa:ReplyTo xmlns:wsa="http://www.w3.org/2005/08/addressing"><wsa:Address>http://www.w3.org/2005/08/addressing/anonymous</wsa:Address></wsa:ReplyTo>` +
		`<wsa:To xmlns:wsa="http://www.w3.org/2005/08/addressing" s:mustUnderstand="1">` + xmlEscape(target) + `</wsa:To>`
	if strings.TrimSpace(referenceParameters) != "" {
		header += referenceParameters
	}
	return header
}

func randomHexID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}

func xmlDuration(duration time.Duration) string {
	seconds := int64(duration.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("PT%dS", seconds)
}

func parseONVIFTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
