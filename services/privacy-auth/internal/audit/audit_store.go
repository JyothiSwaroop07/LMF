// Package audit persists LCS audit records to Cassandra per 3GPP TS 33.127.
package audit

import (
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"go.uber.org/zap"
)

// AuditRecord represents one LCS privacy decision event.
type AuditRecord struct {
	RecordID       gocql.UUID
	Timestamp      time.Time
	Supi           string
	SessionID      string
	LcsClientType  string
	PrivacyClass   string
	Outcome        string
	DenialReason   string
}

// AuditStore writes audit records to Cassandra.
type AuditStore struct {
	session *gocql.Session
	logger  *zap.Logger
}

// NewAuditStore creates an AuditStore connected to the given Cassandra cluster.
func NewAuditStore(cassandraHosts []string, keyspace string, logger *zap.Logger) (*AuditStore, error) {
	cluster := gocql.NewCluster(cassandraHosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.ConnectTimeout = 10 * time.Second

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connect to Cassandra: %w", err)
	}

	return &AuditStore{session: session, logger: logger}, nil
}

// Close releases the Cassandra session.
func (a *AuditStore) Close() {
	a.session.Close()
}

// Write persists an audit record. Non-blocking on error to avoid delaying LCS calls.
func (a *AuditStore) Write(rec AuditRecord) {
	rec.RecordID = gocql.TimeUUID()
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	err := a.session.Query(`
		INSERT INTO lcs_audit (record_id, ts, supi, session_id, lcs_client_type, privacy_class, outcome, denial_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.RecordID, rec.Timestamp, rec.Supi, rec.SessionID,
		rec.LcsClientType, rec.PrivacyClass, rec.Outcome, rec.DenialReason,
	).Exec()

	if err != nil {
		a.logger.Warn("audit record write failed",
			zap.String("supi", rec.Supi),
			zap.String("sessionId", rec.SessionID),
			zap.Error(err),
		)
	}
}
