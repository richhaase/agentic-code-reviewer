package store

import "fmt"

const CurrentSchemaVersion = 1

func validateSchemaVersion(recordKind string, version int) error {
	if version != CurrentSchemaVersion {
		return fmt.Errorf("unsupported %s schema version %d (this build supports version %d)", recordKind, version, CurrentSchemaVersion)
	}
	return nil
}
