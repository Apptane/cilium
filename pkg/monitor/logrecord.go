// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package monitor

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cilium/dns"

	"github.com/cilium/cilium/pkg/monitor/api"
	"github.com/cilium/cilium/pkg/proxy/accesslog"
)

// LogRecordNotify is a proxy access log notification
type LogRecordNotify struct {
	accesslog.LogRecord
}

// Dump prints the message according to the verbosity level specified
func (l *LogRecordNotify) Dump(args *api.DumpArgs) {
	if args.Verbosity == api.JSON {
		l.DumpJSON(args.Buf)
	} else {
		l.DumpInfo(args.Buf)
	}
}

// GetSrc retrieves the source endpoint for the message
func (l *LogRecordNotify) GetSrc() uint16 {
	return uint16(l.SourceEndpoint.ID)
}

// GetDst retrieves the destination endpoint for the message.
func (l *LogRecordNotify) GetDst() uint16 {
	return uint16(l.DestinationEndpoint.ID)
}

// Decode decodes the message in 'data' into the struct.
func (l *LogRecordNotify) Decode(data []byte) error {
	buf := bytes.NewBuffer(data[1:])
	dec := gob.NewDecoder(buf)
	return dec.Decode(l)
}

func (l *LogRecordNotify) direction() string {
	switch l.ObservationPoint {
	case accesslog.Ingress:
		return "<-"
	case accesslog.Egress:
		return "->"
	default:
		return "??"
	}
}

func (l *LogRecordNotify) l7Proto() string {
	if l.HTTP != nil {
		return "http"
	}

	if l.Kafka != nil {
		return "kafka"
	}

	if l.DNS != nil {
		return "dns"
	}

	if l.L7 != nil {
		return l.L7.Proto
	}

	return "unknown-l7"
}

// DumpInfo dumps an access log notification
func (l *LogRecordNotify) DumpInfo(buf *bufio.Writer) {
	switch l.Type {
	case accesslog.TypeRequest:
		fmt.Fprintf(buf, "%s %s %s from %d (%s) to %d (%s), identity %d->%d, verdict %s",
			l.direction(), l.Type, l.l7Proto(), l.SourceEndpoint.ID, l.SourceEndpoint.Labels,
			l.DestinationEndpoint.ID, l.DestinationEndpoint.Labels,
			l.SourceEndpoint.Identity, l.DestinationEndpoint.Identity,
			l.Verdict)

	case accesslog.TypeResponse:
		fmt.Fprintf(buf, "%s %s %s to %d (%s) from %d (%s), identity %d->%d, verdict %s",
			l.direction(), l.Type, l.l7Proto(), l.DestinationEndpoint.ID, l.DestinationEndpoint.Labels,
			l.SourceEndpoint.ID, l.SourceEndpoint.Labels,
			l.SourceEndpoint.Identity, l.DestinationEndpoint.Identity,
			l.Verdict)
	}

	if http := l.HTTP; http != nil {
		url := ""
		if http.URL != nil {
			url = http.URL.String()
		}

		fmt.Fprintf(buf, " %s %s => %d\n", http.Method, url, http.Code)
	}

	if kafka := l.Kafka; kafka != nil {
		fmt.Fprintf(buf, " %s topic %s => %d\n", kafka.APIKey, kafka.Topic.Topic, kafka.ErrorCode)
	}

	if l.DNS != nil {
		types := []string{}
		for _, t := range l.DNS.QTypes {
			types = append(types, dns.TypeToString[t])
		}
		qTypeStr := strings.Join(types, ",")

		switch {
		case l.Type == accesslog.TypeRequest:
			fmt.Fprintf(buf, " DNS %s: %s %s", l.DNS.ObservationSource, l.DNS.Query, qTypeStr)

		case l.Type == accesslog.TypeResponse:
			fmt.Fprintf(buf, " DNS %s: %s %s", l.DNS.ObservationSource, l.DNS.Query, qTypeStr)

			ips := make([]string, 0, len(l.DNS.IPs))
			for _, ip := range l.DNS.IPs {
				ips = append(ips, ip.String())
			}
			fmt.Fprintf(buf, " TTL: %d Answer: '%s'", l.DNS.TTL, strings.Join(ips, ","))

			if len(l.DNS.CNAMEs) > 0 {
				fmt.Fprintf(buf, " CNAMEs: %s", strings.Join(l.DNS.CNAMEs, ","))
			}
		}
		fmt.Fprintf(buf, "\n")
	}

	if l7 := l.L7; l7 != nil {
		status := ""
		for k, v := range l7.Fields {
			if k == "status" {
				status = v
			} else {
				fmt.Fprintf(buf, " %s:%s", k, v)
			}
		}
		if status != "" {
			fmt.Fprintf(buf, " => status:%s", status)
		}
		fmt.Fprintf(buf, "\n")
	}
}

func (l *LogRecordNotify) getJSON() (string, error) {
	v := LogRecordNotifyToVerbose(l)

	ret, err := json.Marshal(v)
	return string(ret), err
}

// DumpJSON prints notification in json format
func (l *LogRecordNotify) DumpJSON(buf *bufio.Writer) {
	resp, err := l.getJSON()
	if err == nil {
		fmt.Fprintln(buf, resp)
	}
}

// LogRecordNotifyVerbose represents a json notification printed by monitor
type LogRecordNotifyVerbose struct {
	Type             string                     `json:"type"`
	ObservationPoint accesslog.ObservationPoint `json:"observationPoint"`
	FlowType         accesslog.FlowType         `json:"flowType"`
	L7Proto          string                     `json:"l7Proto"`
	SrcEpID          uint64                     `json:"srcEpID"`
	SrcEpLabels      []string                   `json:"srcEpLabels"`
	SrcIdentity      uint64                     `json:"srcIdentity"`
	DstEpID          uint64                     `json:"dstEpID"`
	DstEpLabels      []string                   `json:"dstEpLabels"`
	DstIdentity      uint64                     `json:"dstIdentity"`
	Verdict          accesslog.FlowVerdict      `json:"verdict"`
	HTTP             *accesslog.LogRecordHTTP   `json:"http,omitempty"`
	Kafka            *accesslog.LogRecordKafka  `json:"kafka,omitempty"`
	DNS              *accesslog.LogRecordDNS    `json:"dns,omitempty"`
	L7               *accesslog.LogRecordL7     `json:"l7,omitempty"`
}

// LogRecordNotifyToVerbose turns LogRecordNotify into json-friendly Verbose structure
func LogRecordNotifyToVerbose(n *LogRecordNotify) LogRecordNotifyVerbose {
	return LogRecordNotifyVerbose{
		Type:             "logRecord",
		ObservationPoint: n.ObservationPoint,
		FlowType:         n.Type,
		L7Proto:          n.l7Proto(),
		SrcEpID:          n.SourceEndpoint.ID,
		SrcEpLabels:      n.SourceEndpoint.Labels.GetModel(),
		SrcIdentity:      n.SourceEndpoint.Identity,
		DstEpID:          n.DestinationEndpoint.ID,
		DstEpLabels:      n.DestinationEndpoint.Labels.GetModel(),
		DstIdentity:      n.DestinationEndpoint.Identity,
		Verdict:          n.Verdict,
		HTTP:             n.HTTP,
		Kafka:            n.Kafka,
		DNS:              n.DNS,
		L7:               n.L7,
	}
}
