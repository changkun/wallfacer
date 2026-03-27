package store

import "github.com/google/uuid"

// StorageBackend abstracts the three persistence concerns of the store:
// tasks (structured, indexed), events (ordered, append-heavy), and blobs
// (named bytes per task). The Store layer maps domain concepts to these
// primitives (e.g., SaveOversight → SaveBlob(id, "oversight", data)).
type StorageBackend interface {
	// Tasks (structured, indexed)
	Init(taskID uuid.UUID) error
	LoadAll() ([]*Task, error)
	SaveTask(t *Task) error
	RemoveTask(taskID uuid.UUID) error

	// Events (ordered, append-heavy)
	SaveEvent(taskID uuid.UUID, seq int, event TaskEvent) error
	LoadEvents(taskID uuid.UUID) ([]TaskEvent, int64, error)
	CompactEvents(taskID uuid.UUID, events []TaskEvent) error

	// Blobs (named bytes per task)
	SaveBlob(taskID uuid.UUID, key string, data []byte) error
	ReadBlob(taskID uuid.UUID, key string) ([]byte, error)
	DeleteBlob(taskID uuid.UUID, key string) error
	ListBlobs(taskID uuid.UUID, prefix string) ([]string, error)
	ListBlobOwners(key string) ([]uuid.UUID, error)
}
