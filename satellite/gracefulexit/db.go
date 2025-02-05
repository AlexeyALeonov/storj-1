// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package gracefulexit

import (
	"context"
	"time"

	"storj.io/storj/pkg/storj"
)

// Progress represents the persisted graceful exit progress record.
type Progress struct {
	NodeID            storj.NodeID
	BytesTransferred  int64
	PiecesTransferred int64
	PiecesFailed      int64
	UpdatedAt         time.Time
}

// TransferQueueItem represents the persisted graceful exit queue record.
type TransferQueueItem struct {
	NodeID          storj.NodeID
	Path            []byte
	PieceNum        int32
	DurabilityRatio float64
	QueuedAt        time.Time
	RequestedAt     time.Time
	LastFailedAt    time.Time
	LastFailedCode  int
	FailedCount     int
	FinishedAt      time.Time
}

// DB implements CRUD operations for graceful exit service
//
// architecture: Database
type DB interface {
	// IncrementProgress increments transfer stats for a node.
	IncrementProgress(ctx context.Context, nodeID storj.NodeID, bytes int64, successfulTransfers int64, failedTransfers int64) error
	// GetProgress gets a graceful exit progress entry.
	GetProgress(ctx context.Context, nodeID storj.NodeID) (*Progress, error)

	// Enqueue batch inserts graceful exit transfer queue entries it does not exist.
	Enqueue(ctx context.Context, items []TransferQueueItem) error
	// UpdateTransferQueueItem creates a graceful exit transfer queue entry.
	UpdateTransferQueueItem(ctx context.Context, item TransferQueueItem) error
	// DeleteTransferQueueItem deletes a graceful exit transfer queue entry.
	DeleteTransferQueueItem(ctx context.Context, nodeID storj.NodeID, path []byte) error
	// DeleteTransferQueueItem deletes a graceful exit transfer queue entries by nodeID.
	DeleteTransferQueueItems(ctx context.Context, nodeID storj.NodeID) error
	// DeleteFinishedTransferQueueItem deletes finiahed graceful exit transfer queue entries.
	DeleteFinishedTransferQueueItems(ctx context.Context, nodeID storj.NodeID) error
	// GetTransferQueueItem gets a graceful exit transfer queue entry.
	GetTransferQueueItem(ctx context.Context, nodeID storj.NodeID, path []byte) (*TransferQueueItem, error)
	// GetIncomplete gets incomplete graceful exit transfer queue entries ordered by the queued date ascending.
	GetIncomplete(ctx context.Context, nodeID storj.NodeID, limit int, offset int64) ([]*TransferQueueItem, error)
}
