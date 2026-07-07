package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	HTTP               *http.Client
	Username, Password string
}

type DeviceInformation struct{ Manufacturer, Model, FirmwareVersion, SerialNumber, HardwareID string }
type Profile struct {
	Token, Name, Encoding string
	Width, Height         int
}
type Inspection struct {
	DeviceService, MediaService, EventService string
	Information                               DeviceInformation
	Profiles                                  []Profile
	StreamURIs                                map[string]string
}

func NewClient(timeout time.Duration, user, pass string, insecureTLS bool) *Client {
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecureTLS}}
	httpClient := &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Client{HTTP: httpClient, Username: user, Password: pass}
}

func (c *Client) Inspect(ctx context.Context, host string) (Inspection, error) {
	services := deviceServiceCandidates(host)
	var last error
	attempted := make([]string, 0, len(services))
	for _, service := range services {
		attempted = append(attempted, service)
		ins, err := c.inspectService(ctx, service)
		if err == nil {
			return ins, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("sin servicios ONVIF candidatos")
	}
	return Inspection{}, fmt.Errorf("ningún endpoint ONVIF respondió (%s); último error: %w", strings.Join(attempted, ", "), last)
}

func (c *Client) inspectService(ctx context.Context, deviceService string) (Inspection, error) {
	info, err := c.deviceInformation(ctx, deviceService)
	if err != nil {
		return Inspection{}, fmt.Errorf("GetDeviceInformation %s: %w", deviceService, err)
	}
	media, err := c.mediaXAddr(ctx, deviceService)
	if err != nil {
		return Inspection{}, fmt.Errorf("GetCapabilities: %w", err)
	}
	media, err = normalizeEndpointHost(media, deviceService)
	if err != nil {
		return Inspection{}, fmt.Errorf("Media XAddr inválida: %w", err)
	}
	profiles, err := c.profiles(ctx, media)
	if err != nil {
		return Inspection{}, fmt.Errorf("GetProfiles: %w", err)
	}
	events, _ := c.eventXAddr(ctx, deviceService)
	if events != "" {
		events, _ = normalizeEndpointHost(events, deviceService)
	}
	uris := map[string]string{}
	for _, p := range profiles {
		if uri, err := c.streamURI(ctx, media, p.Token); err == nil && uri != "" {
			uris[p.Token] = uri
		}
	}
	if len(uris) == 0 {
		return Inspection{}, errors.New("ONVIF respondió pero no entregó URL RTSP")
	}
	return Inspection{DeviceService: deviceService, MediaService: media, EventService: events, Information: info, Profiles: profiles, StreamURIs: uris}, nil
}

func deviceServiceCandidates(host string) []string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return []string{strings.TrimRight(host, "/")}
	}
	host = strings.Trim(host, "[]")
	defaultHost := host
	if strings.Contains(host, ":") {
		defaultHost = "[" + host + "]"
	}
	return []string{
		"http://" + defaultHost + "/onvif/device_service",
		"http://" + net.JoinHostPort(host, "8899") + "/onvif/device_service",
		"http://" + net.JoinHostPort(host, "8000") + "/onvif/device_service",
	}
}

func normalizeEndpointHost(raw, deviceEndpoint string) (string, error) {
	mediaURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	deviceURL, err := url.Parse(deviceEndpoint)
	if err != nil {
		return "", err
	}
	if !mediaURL.IsAbs() {
		mediaURL = deviceURL.ResolveReference(mediaURL)
	}
	host := deviceURL.Hostname()
	if host == "" {
		return "", errors.New("servicio de dispositivo sin host")
	}
	if port := mediaURL.Port(); port != "" {
		mediaURL.Host = net.JoinHostPort(host, port)
	} else if deviceURL.Port() != "" {
		mediaURL.Host = net.JoinHostPort(host, deviceURL.Port())
	} else {
		mediaURL.Host = host
		if strings.Contains(host, ":") {
			mediaURL.Host = "[" + host + "]"
		}
	}
	if mediaURL.Scheme != "http" && mediaURL.Scheme != "https" {
		return "", errors.New("esquema ONVIF no permitido")
	}
	return mediaURL.String(), nil
}

func (c *Client) deviceInformation(ctx context.Context, endpoint string) (DeviceInformation, error) {
	body := `<tds:GetDeviceInformation xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`
	raw, err := c.soap(ctx, endpoint, "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation", body)
	if err != nil {
		return DeviceInformation{}, err
	}
	var env struct {
		Body struct {
			Response struct {
				Manufacturer    string `xml:"Manufacturer"`
				Model           string `xml:"Model"`
				FirmwareVersion string `xml:"FirmwareVersion"`
				SerialNumber    string `xml:"SerialNumber"`
				HardwareID      string `xml:"HardwareId"`
			} `xml:"GetDeviceInformationResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		return DeviceInformation{}, err
	}
	r := env.Body.Response
	return DeviceInformation{Manufacturer: r.Manufacturer, Model: r.Model, FirmwareVersion: r.FirmwareVersion, SerialNumber: r.SerialNumber, HardwareID: r.HardwareID}, nil
}

func (c *Client) mediaXAddr(ctx context.Context, endpoint string) (string, error) {
	body := `<tds:GetCapabilities xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Category>Media</tds:Category></tds:GetCapabilities>`
	raw, err := c.soap(ctx, endpoint, "http://www.onvif.org/ver10/device/wsdl/GetCapabilities", body)
	if err != nil {
		return "", err
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "XAddr" {
			var v string
			if err := dec.DecodeElement(&v, &se); err == nil && strings.Contains(strings.ToLower(v), "onvif") {
				return strings.TrimSpace(v), nil
			}
		}
	}
	return "", errors.New("respuesta sin Media XAddr")
}

func (c *Client) eventXAddr(ctx context.Context, endpoint string) (string, error) {
	body := `<tds:GetCapabilities xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Category>Events</tds:Category></tds:GetCapabilities>`
	raw, err := c.soap(ctx, endpoint, "http://www.onvif.org/ver10/device/wsdl/GetCapabilities", body)
	if err == nil {
		var envelope struct {
			Body struct {
				Response struct {
					Capabilities struct {
						Events struct {
							XAddr string `xml:"XAddr"`
						} `xml:"Events"`
					} `xml:"Capabilities"`
				} `xml:"GetCapabilitiesResponse"`
			} `xml:"Body"`
		}
		if decodeErr := xml.Unmarshal(raw, &envelope); decodeErr == nil {
			if value := strings.TrimSpace(envelope.Body.Response.Capabilities.Events.XAddr); value != "" {
				return value, nil
			}
		}
	}

	// A few cameras omit Events from GetCapabilities but advertise the
	// service correctly through GetServices.
	servicesBody := `<tds:GetServices xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`
	servicesRaw, servicesErr := c.soap(ctx, endpoint, "http://www.onvif.org/ver10/device/wsdl/GetServices", servicesBody)
	if servicesErr != nil {
		if err != nil {
			return "", err
		}
		return "", servicesErr
	}
	var servicesEnvelope struct {
		Body struct {
			Response struct {
				Services []struct {
					Namespace string `xml:"Namespace"`
					XAddr     string `xml:"XAddr"`
				} `xml:"Service"`
			} `xml:"GetServicesResponse"`
		} `xml:"Body"`
	}
	if decodeErr := xml.Unmarshal(servicesRaw, &servicesEnvelope); decodeErr != nil {
		return "", decodeErr
	}
	for _, service := range servicesEnvelope.Body.Response.Services {
		if strings.TrimSpace(service.Namespace) == "http://www.onvif.org/ver10/events/wsdl" && strings.TrimSpace(service.XAddr) != "" {
			return strings.TrimSpace(service.XAddr), nil
		}
	}
	return "", errors.New("respuesta sin Events XAddr")
}

// DiscoverEventService resolves the native ONVIF event service without
// requiring a media profile or snapshot URL.
func (c *Client) DiscoverEventService(ctx context.Context, host string) (string, error) {
	var last error
	for _, deviceService := range deviceServiceCandidates(host) {
		events, err := c.eventXAddr(ctx, deviceService)
		if err != nil {
			last = err
			continue
		}
		normalized, err := normalizeEndpointHost(events, deviceService)
		if err != nil {
			last = err
			continue
		}
		return normalized, nil
	}
	if last == nil {
		last = errors.New("la cámara no publicó el servicio de eventos")
	}
	return "", last
}

func (c *Client) profiles(ctx context.Context, endpoint string) ([]Profile, error) {
	body := `<trt:GetProfiles xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`
	raw, err := c.soap(ctx, endpoint, "http://www.onvif.org/ver10/media/wsdl/GetProfiles", body)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out []Profile
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Profiles" {
			continue
		}
		p := Profile{}
		for _, a := range se.Attr {
			if a.Name.Local == "token" {
				p.Token = a.Value
			}
		}
		var block struct {
			Name  string `xml:"Name"`
			Video struct {
				Encoding   string `xml:"Encoding"`
				Resolution struct {
					Width  int `xml:"Width"`
					Height int `xml:"Height"`
				} `xml:"Resolution"`
			} `xml:"VideoEncoderConfiguration"`
		}
		if err := dec.DecodeElement(&block, &se); err != nil {
			return nil, err
		}
		p.Name = block.Name
		p.Encoding = block.Video.Encoding
		p.Width = block.Video.Resolution.Width
		p.Height = block.Video.Resolution.Height
		if p.Token != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("respuesta sin perfiles")
	}
	return out, nil
}

func (c *Client) streamURI(ctx context.Context, endpoint, token string) (string, error) {
	body := `<trt:GetStreamUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup><trt:ProfileToken>` + xmlEscape(token) + `</trt:ProfileToken></trt:GetStreamUri>`
	raw, err := c.soap(ctx, endpoint, "http://www.onvif.org/ver10/media/wsdl/GetStreamUri", body)
	if err != nil {
		return "", err
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Uri" {
			var v string
			if err := dec.DecodeElement(&v, &se); err == nil {
				return strings.TrimSpace(v), nil
			}
		}
	}
	return "", errors.New("respuesta sin Uri")
}

func (c *Client) soap(ctx context.Context, endpoint, action, body string) ([]byte, error) {
	return c.soapWithHeader(ctx, endpoint, action, body, "")
}

func (c *Client) soapWithHeader(ctx context.Context, endpoint, action, body, extraHeader string) ([]byte, error) {
	envelope, err := c.envelopeWithHeader(body, extraHeader)
	if err != nil {
		return nil, err
	}
	send := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(envelope))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)
		req.Header.Set("SOAPAction", `"`+action+`"`)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return c.HTTP.Do(req)
	}
	resp, err := send("")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		auth := ""
		lower := strings.ToLower(challenge)
		switch {
		case strings.HasPrefix(lower, "digest "):
			ch, err := parseDigestChallenge(challenge)
			if err != nil {
				return nil, err
			}
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, err
			}
			auth, err = digestAuthorization(ch, http.MethodPost, u.RequestURI(), c.Username, c.Password)
			if err != nil {
				return nil, err
			}
		case strings.HasPrefix(lower, "basic "):
			auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password))
		default:
			return nil, errors.New("método de autenticación ONVIF no soportado")
		}
		resp, err = send(auth)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, compact(raw))
	}
	if bytes.Contains(raw, []byte(":Fault")) || bytes.Contains(raw, []byte("<Fault")) {
		return nil, fmt.Errorf("SOAP Fault: %s", compact(raw))
	}
	return raw, nil
}

func (c *Client) envelope(body string) ([]byte, error) {
	return c.envelopeWithHeader(body, "")
}

func (c *Client) envelopeWithHeader(body, extraHeader string) ([]byte, error) {
	headerContent := strings.TrimSpace(extraHeader)
	if c.Username != "" {
		nonce := make([]byte, 20)
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		created := time.Now().UTC().Format(time.RFC3339Nano)
		h := sha1.New()
		_, _ = h.Write(nonce)
		_, _ = h.Write([]byte(created))
		_, _ = h.Write([]byte(c.Password))
		digest := base64.StdEncoding.EncodeToString(h.Sum(nil))
		security := `<wsse:Security s:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"><wsse:UsernameToken><wsse:Username>` + xmlEscape(c.Username) + `</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + digest + `</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` + base64.StdEncoding.EncodeToString(nonce) + `</wsse:Nonce><wsu:Created>` + created + `</wsu:Created></wsse:UsernameToken></wsse:Security>`
		if headerContent != "" {
			headerContent += "\n"
		}
		headerContent += security
	}
	header := ""
	if headerContent != "" {
		header = `<s:Header>` + headerContent + `</s:Header>`
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">` + header + `<s:Body>` + body + `</s:Body></s:Envelope>`), nil
}

func xmlEscape(v string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}
func compact(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 600 {
		return s[:600] + "..."
	}
	return s
}
