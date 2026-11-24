package eventcollector

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"vms.local/backend/internal/camera"
)

type ruleProbe struct {
	camera.EventReceiver
	inventory camera.RuleInventory
}

func (p *ruleProbe) RuleInventory(context.Context, camera.Source, string) (camera.RuleInventory, error) {
	return p.inventory, nil
}

func TestRuleChangesOnFiveMinutePoll(t *testing.T) {
	store, err := Open(t.TempDir(), strings.Repeat("x", 40), 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := Assignment{Tenant: "tenant", Camera: "camera", Revision: 1, Rules: []Rule{{Kind: "line_crossing", ID: "7"}}}
	if err = store.configure([]Assignment{a}); err != nil {
		t.Fatal(err)
	}
	worker := Worker{store: store}
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		yes, no := true, false
		probe := &ruleProbe{inventory: camera.RuleInventory{Enabled: &yes, Complete: true, Items: []camera.DeviceRule{{ID: "7", Enabled: &yes}}}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go worker.monitorRules(ctx, a, probe)
		synctest.Wait()
		probe.inventory.Items[0].Enabled = &no
		time.Sleep(4 * time.Minute)
		b, err := store.batch()
		if err != nil || len(b.Items) > 0 {
			t.Fatal("unexpected early verification", err, b)
		}
		advance := func() { time.Sleep(5 * time.Minute); synctest.Wait() }
		time.Sleep(time.Minute)
		synctest.Wait()
		advance() // still disabled: one open fault
		probe.inventory.Items = nil
		probe.inventory.Complete = false
		advance()
		probe.inventory.Complete = true
		probe.inventory.Items = []camera.DeviceRule{{ID: "8", Enabled: &yes}}
		advance()
		probe.inventory.Items = []camera.DeviceRule{{ID: "7", Enabled: &yes}}
		advance()
		b, err = store.batch()
		if err != nil {
			t.Fatal(err)
		}
		var codes []string
		for _, r := range b.Items {
			codes = append(codes, r.Code)
		}
		if !slices.Equal(codes, []string{"rule_disabled", "channel_only", "rule_missing", ""}) {
			t.Fatal("incorrect fault transitions", codes)
		}
		if b.Items[0].At.Sub(start) != 5*time.Minute {
			t.Fatal("incorrect first verification interval")
		}
	})
}
