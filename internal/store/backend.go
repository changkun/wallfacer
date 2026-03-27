package store

import "github.com/google/uuid"

// StorageBackend abstracts the three persistence concerns of the store:
// tasks (structured, indexed), events (ordered, append-heavy), and blobs
// (named bytes per task). The Store layer maps domain concepts to these
// primitives (e.g., SaveOversight → SaveBlob(id, "oversight", data)).
type StorageBackend interface {
	// Tasks: structured, indexed task metadata.
	Init(taskID uuid.UUID) error                          // Create the storage location for a new task.
	LoadAll() ([]*Task, error)                             // Read all tasks; called once at startup.
	SaveTask(t *Task) error                                // Atomically persist task metadata.
	RemoveTask(taskID uuid.UUID) error                     // Permanently delete a task and all its data.

	// Events: ordered, append-heavy audit trail per task.
	SaveEvent(taskID uuid.UUID, seq int, event TaskEvent) error      // Persist a single event by sequence number.
	LoadEvents(taskID uuid.UUID) ([]TaskEvent, int64, error)         // Read all events; returns events and highest seq.
	CompactEvents(taskID uuid.UUID, events []TaskEvent) error        // Merge events into compact form and remove originals.

	// Blobs: named byte payloads per task (e.g. oversight.json, outputs/).
	SaveBlob(taskID uuid.UUID, key string, data []byte) error    // Write a named blob; creates parent dirs as needed.
	ReadBlob(taskID uuid.UUID, key string) ([]byte, error)       // Read a named blob; returns os.ErrNotExist if absent.
	DeleteBlob(taskID uuid.UUID, key string) error               // Remove a named blob.
	ListBlobs(taskID uuid.UUID, prefix string) ([]string, error) // List blob keys matching a prefix.
	ListBlobOwners(key string) ([]uuid.UUID, error)              // Find all tasks that have a given blob key.
}
