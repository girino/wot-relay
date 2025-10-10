package sqlite3

import (
	"context"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type SQLite3Backend struct {
	sync.Mutex
	*sqlx.DB
	DatabaseURL         string
	QueryLimit          int
	QueryIDsLimit       int
	QueryAuthorsLimit   int
	QueryKindsLimit     int
	QueryTagsLimit      int
	MaintenanceInterval time.Duration
	SlowQueryThreshold  time.Duration // Log queries slower than this threshold
	maintenanceCtx      context.Context
	maintenanceCancel   context.CancelFunc
	maintenanceStopped  chan struct{}
}

func (b *SQLite3Backend) Close() {
	// Stop periodic maintenance if running
	if b.maintenanceCancel != nil {
		b.maintenanceCancel()
		// Wait for maintenance to stop
		if b.maintenanceStopped != nil {
			<-b.maintenanceStopped
		}
	}
	b.DB.Close()
}

// RunMaintenance executes ANALYZE to update query planner statistics
func (b *SQLite3Backend) RunMaintenance() error {
	// Execute ANALYZE to update query planner statistics
	if _, err := b.DB.Exec("ANALYZE"); err != nil {
		return err
	}

	return nil
}

// Vacuum rebuilds the database, reclaims unused space, and defragments
// Note: VACUUM can be time-consuming on large databases and requires up to 2x disk space temporarily
func (b *SQLite3Backend) Vacuum() error {
	if _, err := b.DB.Exec("VACUUM"); err != nil {
		return err
	}

	return nil
}

// StartPeriodicMaintenance starts a background goroutine that runs database maintenance periodically
func (b *SQLite3Backend) StartPeriodicMaintenance() {
	if b.MaintenanceInterval <= 0 {
		// Maintenance disabled if interval not set
		return
	}

	b.maintenanceCtx, b.maintenanceCancel = context.WithCancel(context.Background())
	b.maintenanceStopped = make(chan struct{})

	go func() {
		defer close(b.maintenanceStopped)

		ticker := time.NewTicker(b.MaintenanceInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := b.RunMaintenance(); err != nil {
					// Log error but continue
					// In a production system, you might want to use a proper logger here
					println("Database maintenance error:", err.Error())
				}
			case <-b.maintenanceCtx.Done():
				return
			}
		}
	}()
}
