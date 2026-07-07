package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPullPointSubscriptionAndMessages(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(string(body), "CreatePullPointSubscription"):
			_, _ = response.Write([]byte(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://www.w3.org/2005/08/addressing" xmlns:tev="http://www.onvif.org/ver10/events/wsdl" xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" xmlns:cam="urn:test-camera">
<s:Body><tev:CreatePullPointSubscriptionResponse><tev:SubscriptionReference><wsa:Address>` + server.URL + `/pull</wsa:Address><wsa:ReferenceParameters><cam:SubscriptionId wsa:IsReferenceParameter="true">42</cam:SubscriptionId></wsa:ReferenceParameters></tev:SubscriptionReference><wsnt:CurrentTime>2026-07-07T12:00:00Z</wsnt:CurrentTime><wsnt:TerminationTime>2026-07-07T12:10:00Z</wsnt:TerminationTime></tev:CreatePullPointSubscriptionResponse></s:Body></s:Envelope>`))
		case strings.Contains(string(body), "PullMessages"):
			if !strings.Contains(string(body), "SubscriptionId") {
				t.Errorf("reference parameter was not copied: %s", body)
			}
			decoder := xml.NewDecoder(bytes.NewReader(body))
			for {
				if _, err := decoder.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Errorf("PullMessages request is not namespace-valid XML: %v; body=%s", err, body)
					break
				}
			}
			_, _ = response.Write([]byte(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tev="http://www.onvif.org/ver10/events/wsdl" xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:tns1="http://www.onvif.org/ver10/topics">
<s:Body><tev:PullMessagesResponse><tev:CurrentTime>2026-07-07T12:00:01Z</tev:CurrentTime><tev:TerminationTime>2026-07-07T12:10:00Z</tev:TerminationTime><wsnt:NotificationMessage><wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:RuleEngine/CellMotionDetector/Motion</wsnt:Topic><wsnt:Message><tt:Message UtcTime="2026-07-07T12:00:01Z" PropertyOperation="Changed"><tt:Source><tt:SimpleItem Name="VideoSourceConfigurationToken" Value="VideoSource_1"/></tt:Source><tt:Data><tt:SimpleItem Name="IsMotion" Value="true"/></tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage></tev:PullMessagesResponse></s:Body></s:Envelope>`))
		case strings.Contains(string(body), "Unsubscribe"):
			_, _ = response.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><UnsubscribeResponse/></s:Body></s:Envelope>`))
		default:
			http.Error(response, "unknown request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewClient(5*time.Second, "", "", false)
	subscription, err := client.CreatePullPointSubscription(context.Background(), server.URL, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Address != server.URL+"/pull" || !strings.Contains(subscription.ReferenceParameters, "SubscriptionId") {
		t.Fatalf("unexpected subscription: %#v", subscription)
	}
	notifications, termination, err := client.PullMessages(context.Background(), subscription, 5*time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 1 || termination.IsZero() {
		t.Fatalf("unexpected pull response: %#v termination=%v", notifications, termination)
	}
	eventType, active, recognized, _ := ClassifyEvent(notifications[0])
	if eventType != "motion" || !active || !recognized {
		t.Fatalf("unexpected classification: type=%q active=%v recognized=%v", eventType, active, recognized)
	}
	if err := client.Unsubscribe(context.Background(), subscription); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyEventIgnoresInitializationAndInactiveState(t *testing.T) {
	initialized := EventNotification{Topic: "tns1:RuleEngine/CellMotionDetector/Motion", PropertyOperation: "Initialized", Items: map[string]string{"IsMotion": "true"}}
	if _, _, recognized, _ := ClassifyEvent(initialized); recognized {
		t.Fatal("initial property state must not create an event")
	}

	inactive := EventNotification{Topic: "tns1:RuleEngine/CellMotionDetector/Motion", PropertyOperation: "Changed", Items: map[string]string{"IsMotion": "false"}}
	if _, active, recognized, _ := ClassifyEvent(inactive); recognized || active {
		t.Fatal("inactive transition must not create an event")
	}
}

func TestClassifyVendorSpecificEvent(t *testing.T) {
	notification := EventNotification{Topic: "vendor:Smart/Tripwire", Items: map[string]string{"Rule": "front-door"}}
	eventType, active, recognized, key := ClassifyEvent(notification)
	if eventType != "onvif" || !active || !recognized || key == "" {
		t.Fatalf("unexpected vendor event classification: %q %v %v %q", eventType, active, recognized, key)
	}
}
