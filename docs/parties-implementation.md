# OCPI Parties Implementation

**Status:** 📋 Planning  
**Created:** 2026-02-05  
**Priority:** Medium  

---

## Overview

The OCPI (Open Charge Point Interface) protocol defines a "party" as an organization that operates within the EV charging ecosystem. Each party is uniquely identified by a combination of `country_code` and `party_id`. Currently, MaEVe CSMS lacks a proper party management system, leading to issues where location country codes are used instead of party country codes.

---

## Problem Statement

### Current Issue

In `manager/store/postgres/locations.go`, we currently extract the country code from the location itself:

```go
// Extract country code and party ID from location
countryCode := location.Country  // ❌ Wrong - should come from party!
```

**Why this is wrong:**
- A party (e.g., ChargePoint Operating in Spain) can have locations in multiple countries
- The party's registered country (`party.country_code`) may differ from the location's country
- OCPI registration and credentials are tied to the party, not individual locations
- This breaks proper OCPI token routing and authorization

### Example Scenario

```
Party: ChargePoint Spain
- country_code: ES
- party_id: CPO
- locations:
  - Location 1: Madrid, Spain (country: ES) ✅ matches
  - Location 2: Lisbon, Portugal (country: PT) ❌ doesn't match
  - Location 3: Paris, France (country: FR) ❌ doesn't match
```

In the current implementation, we would incorrectly use PT or FR as the party's country code for those locations.

---

## OCPI Party Concept

### What is a Party?

From OCPI 2.2.1 specification:

> A Party is the owner of EVSE's, the provider of Charge Detail Records (CDRs), or the provider of Tokens. 
> Each party is identified by a unique combination of country_code and party_id.

### Party Roles

1. **CPO** (Charge Point Operator) - Operates charging stations
2. **eMSP** (e-Mobility Service Provider) - Provides tokens/cards to EV drivers
3. **NSP** (Navigation Service Provider) - Provides route planning
4. **SCSP** (Smart Charging Service Provider) - Manages smart charging

### Party Identification

- **country_code**: ISO-3166 alpha-2 country code (e.g., "ES", "NL", "DE")
- **party_id**: 3-character identifier (e.g., "CPO", "TWK", "ION")
- Combined as `ES-CPO`, `NL-TWK`, etc.

---

## Current Implementation Status

### What Exists

✅ **OCPI Registration** (`manager/store/ocpi.go`)
- `SetRegistrationDetails()` - Stores registration tokens
- `GetRegistrationDetails()` - Retrieves registration by token

✅ **Party Details** (`manager/store/ocpi.go`)
- `SetPartyDetails()` - Stores party information (country_code, party_id, role)
- `GetPartyDetails()` - Retrieves party by country_code + party_id

✅ **Database Schema** (`manager/store/postgres/migrations/000005_create_ocpi_locations.up.sql`)
```sql
CREATE TABLE ocpi_parties (
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    role VARCHAR(10) NOT NULL,
    business_details JSONB,
    PRIMARY KEY (country_code, party_id, role)
);
```

### What's Missing

❌ **Location-Party Association**
- Locations table has `country_code` and `party_id` columns
- But they're extracted from location data, not from party relationship
- No foreign key constraint to `ocpi_parties` table

❌ **Party Context in Store Operations**
- Store methods don't require party context
- No way to filter locations by party
- No validation that party exists

❌ **Party Management API**
- No way to create/manage parties via API
- Parties are only created implicitly during OCPI registration

❌ **Default Party Configuration**
- System doesn't know "our" party (the CSMS operator)
- Can't distinguish between "our" locations and partner locations

---

## Proposed Implementation

### Phase 1: Database & Store Layer

#### 1.1 Update Locations Schema

Add proper foreign key constraint:

```sql
-- Migration: 000007_add_location_party_fk.up.sql
ALTER TABLE locations
ADD CONSTRAINT fk_locations_party
FOREIGN KEY (country_code, party_id)
REFERENCES ocpi_parties(country_code, party_id);
```

#### 1.2 Add Default Party Table

```sql
-- Store system configuration
CREATE TABLE system_config (
    key VARCHAR(50) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Set default party
INSERT INTO system_config (key, value)
VALUES ('default_party', 'ES-TWK');  -- Example: Spain, ThoughtWorks
```

#### 1.3 Update Store Interface

```go
// Add to store/location.go
type LocationStore interface {
    // ... existing methods ...
    
    // SetLocationForParty explicitly associates location with a party
    SetLocationForParty(ctx context.Context, countryCode, partyID string, location *Location) error
    
    // GetLocationsByParty retrieves all locations for a party
    GetLocationsByParty(ctx context.Context, countryCode, partyID string, offset, limit int) ([]*Location, error)
}
```

#### 1.4 Update Party Store Interface

```go
// Add to store/ocpi.go
type PartyStore interface {
    // ... existing methods ...
    
    // GetDefaultParty returns the system's default party
    GetDefaultParty(ctx context.Context) (countryCode, partyID string, err error)
    
    // SetDefaultParty sets the system's default party
    SetDefaultParty(ctx context.Context, countryCode, partyID string) error
    
    // ListParties returns all registered parties
    ListParties(ctx context.Context, role string, offset, limit int) ([]*PartyDetails, error)
}
```

### Phase 2: Configuration

#### 2.1 Add Party Config to manager/config

```toml
[ocpi]
# ... existing config ...

# Default party for this CSMS instance
default_party.country_code = "ES"
default_party.party_id = "TWK"
default_party.role = "CPO"
default_party.business_details.name = "MaEVe CSMS"
default_party.business_details.website = "https://example.com"
```

#### 2.2 Load Default Party on Startup

```go
// In manager startup code
func initializeDefaultParty(ctx context.Context, store store.Engine, cfg config.Config) error {
    // Check if default party exists
    _, _, err := store.GetDefaultParty(ctx)
    if err == store.ErrNotFound {
        // Create default party
        err = store.SetPartyDetails(ctx, &store.PartyDetails{
            CountryCode: cfg.OCPI.DefaultParty.CountryCode,
            PartyID:     cfg.OCPI.DefaultParty.PartyID,
            Role:        cfg.OCPI.DefaultParty.Role,
            BusinessDetails: cfg.OCPI.DefaultParty.BusinessDetails,
        })
        if err != nil {
            return fmt.Errorf("failed to create default party: %w", err)
        }
        
        // Set as system default
        err = store.SetDefaultParty(ctx, 
            cfg.OCPI.DefaultParty.CountryCode,
            cfg.OCPI.DefaultParty.PartyID)
        if err != nil {
            return fmt.Errorf("failed to set default party: %w", err)
        }
    }
    return nil
}
```

### Phase 3: Update Locations Logic

#### 3.1 Fix SetLocation to Use Party

```go
// In manager/store/postgres/locations.go
func (s *Store) SetLocation(ctx context.Context, location *store.Location) error {
    // Get default party (or require party as parameter)
    countryCode, partyID, err := s.GetDefaultParty(ctx)
    if err != nil {
        return fmt.Errorf("failed to get default party: %w", err)
    }
    
    // Serialize location data to JSON
    locationData, err := json.Marshal(location)
    if err != nil {
        return fmt.Errorf("failed to marshal location data: %w", err)
    }
    
    // Use party's country code and party ID
    _, err = s.queries.SetLocation(ctx, SetLocationParams{
        LocationID:  location.Id,
        CountryCode: countryCode,  // ✅ From party
        PartyID:     partyID,      // ✅ From party
        LocationData: locationData,
    })
    
    return err
}
```

#### 3.2 Alternative: Add Party Parameter

```go
func (s *Store) SetLocationForParty(ctx context.Context, 
    countryCode, partyID string, location *store.Location) error {
    
    // Verify party exists
    _, err := s.GetPartyDetails(ctx, countryCode, partyID)
    if err != nil {
        return fmt.Errorf("party %s-%s not found: %w", countryCode, partyID, err)
    }
    
    // ... rest of implementation
}
```

### Phase 4: API & Management

#### 4.1 Party Management Endpoints

```go
// GET /ocpi/parties
// List all registered parties

// GET /ocpi/parties/{country_code}/{party_id}
// Get party details

// POST /ocpi/parties
// Create/update party (admin only)

// GET /ocpi/parties/default
// Get system default party

// PUT /ocpi/parties/default
// Set system default party (admin only)
```

#### 4.2 Update Location Endpoints

```go
// POST /ocpi/cpo/2.2.1/locations/{party}
// Create location for specific party

// GET /ocpi/cpo/2.2.1/locations?party={country_code}-{party_id}
// Filter locations by party
```

---

## Migration Strategy

### For Existing Deployments

1. **Add default party configuration** to config file
2. **Run migration** to add FK constraint (may fail if data is inconsistent)
3. **Data cleanup script**:
   ```sql
   -- Find locations without valid party
   SELECT location_id, country_code, party_id
   FROM locations
   WHERE NOT EXISTS (
       SELECT 1 FROM ocpi_parties p
       WHERE p.country_code = locations.country_code
       AND p.party_id = locations.party_id
   );
   
   -- Update to use default party
   UPDATE locations
   SET country_code = 'ES',  -- Your default
       party_id = 'TWK'      -- Your default
   WHERE NOT EXISTS (
       SELECT 1 FROM ocpi_parties p
       WHERE p.country_code = locations.country_code
       AND p.party_id = locations.party_id
   );
   ```

4. **Apply FK constraint**
5. **Update application config** with default party details

---

## Testing Strategy

### Unit Tests

```go
func TestSetLocationForParty(t *testing.T) {
    // Test setting location for valid party
    // Test rejecting location for non-existent party
    // Test default party fallback
}

func TestGetLocationsByParty(t *testing.T) {
    // Test filtering by party
    // Test party with multiple locations
    // Test party with no locations
}
```

### Integration Tests

```go
func TestOCPIRegistrationCreatesParty(t *testing.T) {
    // Register via OCPI
    // Verify party is created
    // Create location for that party
    // Verify location is associated correctly
}
```

---

## Implementation Tasks

### Task 1: Database Schema Updates
- [ ] Create migration 000007 for FK constraint
- [ ] Create system_config table
- [ ] Add default party configuration
- [ ] Write data cleanup script for existing deployments

### Task 2: Store Interface Updates
- [ ] Add `GetDefaultParty()` to OcpiStore
- [ ] Add `SetDefaultParty()` to OcpiStore
- [ ] Add `ListParties()` to OcpiStore
- [ ] Add `SetLocationForParty()` to LocationStore
- [ ] Add `GetLocationsByParty()` to LocationStore

### Task 3: PostgreSQL Implementation
- [ ] Implement default party storage (system_config)
- [ ] Implement `GetDefaultParty()`
- [ ] Implement `SetDefaultParty()`
- [ ] Implement `ListParties()`
- [ ] Update `SetLocation()` to use party context
- [ ] Implement `SetLocationForParty()`
- [ ] Implement `GetLocationsByParty()`

### Task 4: Configuration
- [ ] Add default party config structure
- [ ] Add config validation
- [ ] Add startup initialization for default party
- [ ] Update example config files

### Task 5: API Updates
- [ ] Add party management endpoints
- [ ] Update location endpoints to support party filtering
- [ ] Add admin authentication for party management
- [ ] Update OpenAPI spec

### Task 6: Testing
- [ ] Unit tests for store methods
- [ ] Integration tests for party management
- [ ] Migration testing with existing data
- [ ] End-to-end OCPI flow testing

### Task 7: Documentation
- [ ] Update OCPI documentation
- [ ] Add party management guide
- [ ] Update deployment guide with migration steps
- [ ] Add troubleshooting section

---

## Dependencies

- **OCPI Specification 2.2.1**: Understanding of party roles and identification
- **PostgreSQL Store**: Foundation for implementation
- **OCPI API**: Endpoints for party registration
- **Configuration System**: Storing default party settings

---

## Success Criteria

✅ **Correct Party Association**
- Locations are associated with the correct party
- Location country ≠ party country is handled correctly

✅ **OCPI Compliance**
- Party identification follows OCPI 2.2.1 spec
- Credentials endpoint works with multiple parties

✅ **Multi-Party Support**
- System can handle multiple parties (CPO, eMSP, etc.)
- Locations can be filtered by party
- Default party is used when not specified

✅ **Backward Compatibility**
- Existing deployments can migrate without data loss
- Configuration is optional (sane defaults)

---

## References

- [OCPI 2.2.1 Specification](https://github.com/ocpi/ocpi)
- [OCPI Specification](https://github.com/ocpi/ocpi)
- [Current LocationStore Interface](../manager/store/location.go)
- [Current OcpiStore Interface](../manager/store/ocpi.go)
- [PostgreSQL Locations Implementation](../manager/store/postgres/locations.go)

---

## Notes

- This is a **foundational feature** for proper OCPI support
- Should be prioritized if planning to support multiple parties or OCPI roaming
- Can be implemented incrementally (database → store → API)
- Existing single-party deployments continue working with default party

---

**Created by:** Patricio (AI Assistant)  
**Last Updated:** 2026-02-05
