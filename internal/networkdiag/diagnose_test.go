package networkdiag

import (
	"testing"

	fragrtsp "fragata/internal/rtsp"
)

func TestExplainContainerTimeout(t *testing.T) {
	summary, recommendation := explain(Report{
		InContainer: true,
		PortChecks: []fragrtsp.PortCheck{
			{Port: 554, State: "timeout"},
			{Port: 80, State: "timeout"},
		},
	})
	if summary == "" || recommendation == "" {
		t.Fatal("expected diagnostic explanation")
	}
}

func TestExplainOpenPort(t *testing.T) {
	summary, _ := explain(Report{PortChecks: []fragrtsp.PortCheck{{Port: 554, State: "open", Reachable: true}}})
	if summary != "La cámara acepta conexiones TCP desde Fragata." {
		t.Fatalf("unexpected summary: %s", summary)
	}
}
