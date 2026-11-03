package incident

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"os"

	"github.com/tonquoc0407/capybara/internal/store"
)

var packetTemplate = template.Must(template.New("packet").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>capybara incident {{.Packet.RunID}}</title><style>
body{background:#161616;color:#d0d0d0;font:14px/1.5 ui-monospace,monospace;max-width:1100px;margin:2rem auto;padding:0 1rem}
h1,h2{color:#d78700}section{border-top:1px solid #454545;padding:1rem 0}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#202020;padding:1rem}
.error{color:#ff5f5f}.muted{color:#999}
</style></head><body>
<h1>Incident {{.Packet.RunID}}</h1>
<p>Project {{.Packet.Context.Project}} · {{.Packet.Context.Environment}} · release {{.Packet.Context.Release}} · status {{.Packet.Context.Status}}</p>
<p class="muted">Fingerprint {{.Packet.Fingerprint}} groups similar traces; it does not prove a shared cause.</p>
<section><h2>Observed timeline</h2>{{range .Packet.Observed.Spans}}<p>{{.StartedAt}} · {{.Kind}} {{.Name}} · {{.Status}} · {{.ID}}</p>{{end}}</section>
<section><h2>Recorded content</h2>{{range $span,$rows := .Packet.Observed.Contents}}<h3>Span {{$span}}</h3>{{range $rows}}<p>{{.Role}}</p><pre>{{.Body}}</pre>{{end}}{{end}}</section>
<section><h2>Linked logs</h2>{{range .Packet.Observed.Logs}}<p>{{.ObservedAt}} · {{.Severity}} · {{.SpanID}}</p><pre>{{.Body}}</pre>{{end}}</section>
<section><h2>Detector hypotheses</h2>{{range .Packet.Hypotheses}}<p class="error">{{.Type}} · {{.Severity}} · span {{.SpanID}}</p><pre>{{.Detail}}</pre>{{end}}</section>
<section><h2>Cost provenance</h2>{{range .Packet.Costs}}<p>{{.SpanID}} · {{.Kind}} · {{.PriceSource}} · {{.PriceVersion}}</p>{{end}}</section>
{{if .Packet.Comparison}}<section><h2>Before and after</h2><p>Parent {{.Packet.Comparison.ParentRunID}} · {{.Packet.Comparison.ParentStatus}} → {{.Packet.Comparison.CurrentStatus}} · first divergence {{.Packet.Comparison.FirstDivergence}}</p></section>{{end}}
<section><h2>Missing evidence</h2><p>Replay inputs recorded: {{.Packet.ReplayInputsRecorded}}</p>{{range .Packet.Missing}}<p>{{.}}</p>{{end}}</section>
<section><h2>Scores</h2>{{range .Packet.Scores}}<p>{{.Name}} · {{.Value}} · {{.Source}}</p>{{end}}</section>
{{if .Fixture}}<section><h2>Regression fixture</h2><pre>{{.Fixture}}</pre></section>{{end}}
</body></html>`))

func PacketHTML(packet Packet) ([]byte, error) {
	fixture := ""
	if packet.Fixture != nil {
		raw, err := json.MarshalIndent(packet.Fixture, "", "  ")
		if err != nil {
			return nil, err
		}
		fixture = string(raw)
	}
	var out bytes.Buffer
	err := packetTemplate.Execute(&out, struct {
		Packet  Packet
		Fixture string
	}{packet, fixture})
	return out.Bytes(), err
}

func WritePacketHTML(ctx context.Context, st *store.Store, runID, path string) error {
	packet, err := BuildPacket(ctx, st, runID)
	if err != nil {
		return err
	}
	body, err := PacketHTML(packet)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
