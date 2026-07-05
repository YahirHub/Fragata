package onvif

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"net"
	"sort"
	"strings"
	"time"
)

type DiscoveredDevice struct {
	EndpointReference string   `json:"endpoint_reference,omitempty"`
	XAddrs            []string `json:"xaddrs"`
	Types             []string `json:"types,omitempty"`
	Scopes            []string `json:"scopes,omitempty"`
	RemoteAddress     string   `json:"remote_address"`
}

type probeEnvelope struct {
	Body struct {
		ProbeMatches struct {
			Matches []struct {
				EndpointReference struct {
					Address string `xml:"Address"`
				} `xml:"EndpointReference"`
				Types  string `xml:"Types"`
				Scopes string `xml:"Scopes"`
				XAddrs string `xml:"XAddrs"`
			} `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	} `xml:"Body"`
}

func Discover(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	messageID := "urn:uuid:" + hex.EncodeToString(id)
	probe := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<e:Header><w:MessageID>` + messageID + `</w:MessageID><w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To><w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header>` +
		`<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`
	target := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 3702}
	if _, err := conn.WriteToUDP([]byte(probe), target); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	byKey := map[string]DiscoveredDevice{}
	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return values(byKey), ctx.Err()
		default:
		}
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				break
			}
			return values(byKey), err
		}
		var env probeEnvelope
		if xml.Unmarshal(buf[:n], &env) != nil {
			continue
		}
		for _, m := range env.Body.ProbeMatches.Matches {
			xaddrs := strings.Fields(m.XAddrs)
			if len(xaddrs) == 0 {
				continue
			}
			key := m.EndpointReference.Address
			if key == "" {
				key = xaddrs[0]
			}
			dev := DiscoveredDevice{EndpointReference: m.EndpointReference.Address, XAddrs: xaddrs, Types: strings.Fields(m.Types), Scopes: strings.Fields(m.Scopes), RemoteAddress: addr.IP.String()}
			byKey[key] = dev
		}
	}
	return values(byKey), nil
}

func values(m map[string]DiscoveredDevice) []DiscoveredDevice {
	out := make([]DiscoveredDevice, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RemoteAddress < out[j].RemoteAddress })
	return out
}
