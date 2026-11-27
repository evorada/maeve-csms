// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// assignLegacyOrgStatements move the rows of the legacy org ($2) to another org ($1),
// one statement for each table whose rows belong to an org (column org_id).
//
// A token of the legacy org stays where it is when the other org already has a token
// with the same country code, party id and UID (tokens are unique for these): moving
// it would fail, and nothing says which of the two tokens is the right one.
var assignLegacyOrgStatements = []string{
	`UPDATE charge_stations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE locations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE tokens SET org_id = $1 WHERE org_id = $2
	   AND NOT EXISTS (
	       SELECT 1 FROM tokens other
	       WHERE other.org_id = $1 AND other.country_code = tokens.country_code
	         AND other.party_id = tokens.party_id AND other.uid = tokens.uid)`,
	`UPDATE transactions SET org_id = $1 WHERE org_id = $2`,
	`UPDATE reservations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE charging_profiles SET org_id = $1 WHERE org_id = $2`,
	`UPDATE ocpi_registrations SET org_id = $1 WHERE org_id = $2`,
	`UPDATE ocpi_parties SET org_id = $1 WHERE org_id = $2`,
}

// legacyTokensLogged is the number of tokens that AssignLegacyOrg names in its warning
// about the tokens that it could not move.
const legacyTokensLogged = 20

// AssignLegacyOrg gives every row that belongs to the legacy org (store.LegacyOrgId:
// the rows that were there before orgs, and the rows that were written without an
// org) to orgId. It returns the number of rows that changed. All tables change in one
// database transaction. Calling it again, or from several instances at once, is safe.
//
// A token of the legacy org is not moved when orgId already has a token with the same
// country code, party id and UID. Both tokens are kept as they are: the token of
// orgId stays the one that the charge stations of orgId are authorised with, and the
// token of the legacy org stays in the legacy org. Every call logs a warning that
// names these tokens, until one of each pair has been removed by hand.
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

	// the tokens that are still in the legacy org are the ones that could not move
	rows, err := tx.Query(ctx,
		`SELECT country_code || '-' || party_id || '-' || uid FROM tokens
		 WHERE org_id = $1 ORDER BY country_code, party_id, uid`, legacy)
	if err != nil {
		return 0, fmt.Errorf("failed to list the tokens of the legacy org: %w", err)
	}
	kept, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("failed to list the tokens of the legacy org: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	if len(kept) > 0 {
		named := kept
		if len(named) > legacyTokensLogged {
			named = named[:legacyTokensLogged]
		}
		slog.Warn("tokens of the legacy org were not moved: the org already has a token with the same country code, party id and UID; "+
			"both tokens are kept, compare them and delete one of each pair",
			"org_id", orgId, "legacy_org_id", store.LegacyOrgId, "count", len(kept), "tokens", named)
	}
	return total, nil
}
