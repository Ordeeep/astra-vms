//go:build astra_dev

package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// SimulateAnalyticEvents inserts a demo batch through the production write path.
// It can run against a migrated development database while the core is running.
func SimulateAnalyticEvents(ctx context.Context, databaseURL, tenant, cameraRef string) (int, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return 0, err
	}
	config.RuntimeParams["search_path"] = "vms"
	db, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return 0, err
	}
	defer db.Close(ctx)
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	camera, err := resolveCameraKey(ctx, tx, tenant, cameraRef)
	if err != nil {
		return 0, err
	}
	cameraAt := time.Now().Add(-7 * time.Minute).UTC()
	alert := analyticAlert{kind: "intrusion", ruleName: "Área monitorada", cameraAt: &cameraAt, simulated: true}
	events := []analyticAlert{
		alert,
		alert,
		{kind: "line_crossing", ruleName: "Linha de acesso", simulated: true},
	}
	for _, event := range events {
		if _, err := storeAnalyticEvent(ctx, tx, tenant, camera, event); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(events), nil
}
