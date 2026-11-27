// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// assignLegacyOrgStatements move the rows of the legacy org ($2) to another org ($1),
// one statement for each table whose rows belong to an org (column org_id).
var assignLegacyOrgStatements = []string{
	`UPDATE charge_stations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE locations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE tokens SET org_id = $1 WHERE org_id = $2`,
	`UPDATE transactions SET org_id = $1 WHERE org_id = $2`,
	`UPDATE reservations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE charging_profiles SET org_id = $1 WHERE org_id = $2`,
	`UPDATE ocpi_registrations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE ocpi_parties SET org_id = $1 WHERE org_id = $2`,
}

// AssignLegacyOrg gives every row that belongs to the legacy org (store.LegacyOrgId:
// the rows that were there before orgs, and the rows that were written without an
// org) to orgId. It returns the number of rows that changed. All tables change in one
// database transaction. Calling it again, or from several instances at once, is safe.
//
// It is not part of store.Engine: only PostgreSQL stores the org of a row.
func (s *Store) AssignLegacyOrg(ctx context.Context, orgId string) (int64, error) {
	id, err := pgUUID(orgId)
	if err != nil {
		return 0, fmt.Errorf("invalid org id %q: %w", orgId, err)
	}
	legacy, err := pgUUID(store.LegacyOrgId)
	if err != nil {
		return 0, err
	}
	if id == legacy {
		return 0, nil
	}

	tx, err := s.writePool().Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var total int64
	for _, statement := range assignLegacyOrgStatements {
		tag, err := tx.Exec(ctx, statement, id, legacy)
		if err != nil {
			return 0, fmt.Errorf("failed to assign the legacy org (%s): %w", statement, err)
		}
		total += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return total, nil
}
