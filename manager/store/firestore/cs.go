// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type chargeStation struct {
	SecurityProfile        int    `firestore:"prof"`
	Base64SHA256Password   string `firestore:"pwd"`
	InvalidUsernameAllowed bool   `firestore:"inv"`
}

func (s *Store) SetChargeStationAuth(ctx context.Context, chargeStationId string, auth *store.ChargeStationAuth) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStation/%s", chargeStationId))
	_, err := csRef.Set(ctx, &chargeStation{
		SecurityProfile:        int(auth.SecurityProfile),
		Base64SHA256Password:   auth.Base64SHA256Password,
		InvalidUsernameAllowed: auth.InvalidUsernameAllowed,
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) LookupChargeStationAuth(ctx context.Context, chargeStationId string) (*store.ChargeStationAuth, error) {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStation/%s", chargeStationId))
	snap, err := csRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup charge station %s: %w", chargeStationId, err)
	}
	var csData chargeStation
	if err = snap.DataTo(&csData); err != nil {
		return nil, fmt.Errorf("map charge station %s: %w", chargeStationId, err)
	}

	// Validate SecurityProfile is within valid range for int8
	if csData.SecurityProfile < 0 || csData.SecurityProfile > 127 {
		return nil, fmt.Errorf("security profile value %d is out of valid range", csData.SecurityProfile)
	}

	return &store.ChargeStationAuth{
		SecurityProfile:        store.SecurityProfile(csData.SecurityProfile),
		Base64SHA256Password:   csData.Base64SHA256Password,
		InvalidUsernameAllowed: csData.InvalidUsernameAllowed,
	}, nil
}

type chargeStationSetting struct {
	Value     string    `firestore:"v"`
	Status    string    `firestore:"s"`
	SendAfter time.Time `firestore:"u"`
}

func (s *Store) UpdateChargeStationSettings(ctx context.Context, chargeStationId string, settings *store.ChargeStationSettings) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationSettings/%s", chargeStationId))
	var set = make(map[string]*chargeStationSetting)
	for k, v := range settings.Settings {
		set[k] = &chargeStationSetting{
			Value:  v.Value,
			Status: string(v.Status),
		}
	}
	_, err := csRef.Set(ctx, set, firestore.MergeAll)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) LookupChargeStationSettings(ctx context.Context, chargeStationId string) (*store.ChargeStationSettings, error) {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationSettings/%s", chargeStationId))
	snap, err := csRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup charge station settings %s: %w", chargeStationId, err)
	}
	var csData map[string]*chargeStationSetting
	if err = snap.DataTo(&csData); err != nil {
		return nil, fmt.Errorf("map charge station settings %s: %w", chargeStationId, err)
	}
	var settings = mapChargeStationSettings(csData)
	return &store.ChargeStationSettings{
		ChargeStationId: chargeStationId,
		Settings:        settings,
	}, nil
}

func (s *Store) DeleteChargeStationSettings(ctx context.Context, chargeStationId string) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationSettings/%s", chargeStationId))
	_, err := csRef.Delete(ctx)
	if err != nil {
		return err
	}
	return nil
}

func mapChargeStationSettings(csData map[string]*chargeStationSetting) map[string]*store.ChargeStationSetting {
	var settings = make(map[string]*store.ChargeStationSetting)
	for k, v := range csData {
		settings[k] = &store.ChargeStationSetting{
			Value:     v.Value,
			Status:    store.ChargeStationSettingStatus(v.Status),
			SendAfter: v.SendAfter,
		}
	}
	return settings
}

func (s *Store) ListChargeStationSettings(ctx context.Context, pageSize int, previousCsId string) ([]*store.ChargeStationSettings, error) {
	var chargeStationSettings []*store.ChargeStationSettings
	var docIt *firestore.DocumentIterator
	if previousCsId == "" {
		docIt = s.client.Collection("ChargeStationSettings").OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(pageSize).Documents(ctx)
	} else {
		docIt = s.client.Collection("ChargeStationSettings").OrderBy(firestore.DocumentID, firestore.Asc).
			StartAfter(previousCsId).Limit(pageSize).Documents(ctx)
	}
	snaps, err := docIt.GetAll()
	if err != nil {
		return nil, fmt.Errorf("list charge station settings: %w", err)
	}
	for _, snap := range snaps {
		var settings map[string]*chargeStationSetting
		if err = snap.DataTo(&settings); err != nil {
			return nil, fmt.Errorf("map charge station settings: %w", err)
		}
		chargeStationSetting := mapChargeStationSettings(settings)
		chargeStationSettings = append(chargeStationSettings, &store.ChargeStationSettings{
			ChargeStationId: snap.Ref.ID,
			Settings:        chargeStationSetting,
		})
	}
	return chargeStationSettings, nil
}

type chargeStationInstallCertificate struct {
	Type      string    `firestore:"t"`
	Data      string    `firestore:"d"`
	Status    string    `firestore:"s"`
	SendAfter time.Time `firestore:"u"`
}

func mapChargeStationInstallCertificates(certificates map[string]*chargeStationInstallCertificate) []*store.ChargeStationInstallCertificate {
	var certs []*store.ChargeStationInstallCertificate
	for id, c := range certificates {
		certs = append(certs, &store.ChargeStationInstallCertificate{
			CertificateType:               store.CertificateType(c.Type),
			CertificateId:                 id,
			CertificateData:               c.Data,
			CertificateInstallationStatus: store.CertificateInstallationStatus(c.Status),
			SendAfter:                     c.SendAfter,
		})
	}
	return certs
}

func (s *Store) UpdateChargeStationInstallCertificates(ctx context.Context, chargeStationId string, certificates *store.ChargeStationInstallCertificates) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationInstallCertificates/%s", chargeStationId))
	var set = make(map[string]*chargeStationInstallCertificate)
	for _, c := range certificates.Certificates {
		set[c.CertificateId] = &chargeStationInstallCertificate{
			Type:      string(c.CertificateType),
			Data:      c.CertificateData,
			Status:    string(c.CertificateInstallationStatus),
			SendAfter: c.SendAfter,
		}
	}
	_, err := csRef.Set(ctx, set, firestore.MergeAll)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) LookupChargeStationInstallCertificates(ctx context.Context, chargeStationId string) (*store.ChargeStationInstallCertificates, error) {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationInstallCertificates/%s", chargeStationId))
	snap, err := csRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup charge station install certificates %s: %w", chargeStationId, err)
	}
	var csData map[string]*chargeStationInstallCertificate
	if err = snap.DataTo(&csData); err != nil {
		return nil, fmt.Errorf("map charge station install certificates %s: %w", chargeStationId, err)
	}
	var certs = mapChargeStationInstallCertificates(csData)
	return &store.ChargeStationInstallCertificates{
		ChargeStationId: chargeStationId,
		Certificates:    certs,
	}, nil
}

func (s *Store) ListChargeStationInstallCertificates(ctx context.Context, pageSize int, previousCsId string) ([]*store.ChargeStationInstallCertificates, error) {
	var installCerts []*store.ChargeStationInstallCertificates
	var docIt *firestore.DocumentIterator
	if previousCsId == "" {
		docIt = s.client.Collection("ChargeStationInstallCertificates").OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(pageSize).Documents(ctx)
	} else {
		docIt = s.client.Collection("ChargeStationInstallCertificates").OrderBy(firestore.DocumentID, firestore.Asc).
			StartAfter(previousCsId).Limit(pageSize).Documents(ctx)
	}
	snaps, err := docIt.GetAll()
	if err != nil {
		return nil, fmt.Errorf("list charge station install certificates: %w", err)
	}
	for _, snap := range snaps {
		var certs map[string]*chargeStationInstallCertificate
		if err = snap.DataTo(&certs); err != nil {
			return nil, fmt.Errorf("map charge station install certificates: %w", err)
		}
		installCert := mapChargeStationInstallCertificates(certs)
		installCerts = append(installCerts, &store.ChargeStationInstallCertificates{
			ChargeStationId: snap.Ref.ID,
			Certificates:    installCert,
		})
	}
	return installCerts, nil
}

type chargeStationRuntimeDetails struct {
	OcppVersion string `firestore:"v"`
}

func (s *Store) SetChargeStationRuntimeDetails(ctx context.Context, chargeStationId string, details *store.ChargeStationRuntimeDetails) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationRuntimeDetails/%s", chargeStationId))
	_, err := csRef.Set(ctx, &chargeStationRuntimeDetails{
		OcppVersion: details.OcppVersion,
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) LookupChargeStationRuntimeDetails(ctx context.Context, chargeStationId string) (*store.ChargeStationRuntimeDetails, error) {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationRuntimeDetails/%s", chargeStationId))
	snap, err := csRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup charge station runtime details %s: %w", chargeStationId, err)
	}
	var csData chargeStationRuntimeDetails
	if err = snap.DataTo(&csData); err != nil {
		return nil, fmt.Errorf("map charge station runtime details %s: %w", chargeStationId, err)
	}
	return &store.ChargeStationRuntimeDetails{
		OcppVersion: csData.OcppVersion,
	}, nil
}

type chargeStationTriggerMessage struct {
	Type        string    `firestore:"t"`
	ConnectorId *int      `firestore:"c,omitempty"`
	Status      string    `firestore:"s"`
	SendAfter   time.Time `firestore:"u"`
}

func (s *Store) SetChargeStationTriggerMessage(ctx context.Context, chargeStationId string, triggerMessage *store.ChargeStationTriggerMessage) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationTriggerMessage/%s", chargeStationId))
	_, err := csRef.Set(ctx, &chargeStationTriggerMessage{
		Type:        string(triggerMessage.TriggerMessage),
		ConnectorId: triggerMessage.ConnectorId,
		Status:      string(triggerMessage.TriggerStatus),
		SendAfter:   triggerMessage.SendAfter,
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) DeleteChargeStationTriggerMessage(ctx context.Context, chargeStationId string) error {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationTriggerMessage/%s", chargeStationId))
	_, err := csRef.Delete(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) LookupChargeStationTriggerMessage(ctx context.Context, chargeStationId string) (*store.ChargeStationTriggerMessage, error) {
	csRef := s.client.Doc(fmt.Sprintf("ChargeStationTriggerMessage/%s", chargeStationId))
	snap, err := csRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup charge station trigger message %s: %w", chargeStationId, err)
	}
	var csData chargeStationTriggerMessage
	if err = snap.DataTo(&csData); err != nil {
		return nil, fmt.Errorf("map charge station trigger message %s: %w", chargeStationId, err)
	}
	return &store.ChargeStationTriggerMessage{
		ChargeStationId: chargeStationId,
		TriggerMessage:  store.TriggerMessage(csData.Type),
		ConnectorId:     csData.ConnectorId,
		TriggerStatus:   store.TriggerStatus(csData.Status),
		SendAfter:       csData.SendAfter,
	}, nil
}

func (s *Store) ListChargeStationTriggerMessages(ctx context.Context, pageSize int, previousCsId string) ([]*store.ChargeStationTriggerMessage, error) {
	var triggerMessages []*store.ChargeStationTriggerMessage
	var docIt *firestore.DocumentIterator
	if previousCsId == "" {
		docIt = s.client.Collection("ChargeStationTriggerMessage").OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(pageSize).Documents(ctx)
	} else {
		docIt = s.client.Collection("ChargeStationTriggerMessage").OrderBy(firestore.DocumentID, firestore.Asc).
			StartAfter(previousCsId).Limit(pageSize).Documents(ctx)
	}
	snaps, err := docIt.GetAll()
	if err != nil {
		return nil, fmt.Errorf("list charge station trigger messages: %w", err)
	}
	for _, snap := range snaps {
		var triggerMessage chargeStationTriggerMessage
		if err = snap.DataTo(&triggerMessage); err != nil {
			return nil, fmt.Errorf("map charge station trigger message: %w", err)
		}
		triggerMessages = append(triggerMessages, &store.ChargeStationTriggerMessage{
			ChargeStationId: snap.Ref.ID,
			TriggerMessage:  store.TriggerMessage(triggerMessage.Type),
			ConnectorId:     triggerMessage.ConnectorId,
			TriggerStatus:   store.TriggerStatus(triggerMessage.Status),
			SendAfter:       triggerMessage.SendAfter,
		})
	}
	return triggerMessages, nil
}

type resetRequest struct {
	Type      string    `firestore:"type"`
	Status    string    `firestore:"status"`
	CreatedAt time.Time `firestore:"createdAt"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

func (s *Store) SetResetRequest(ctx context.Context, chargeStationId string, request *store.ResetRequest) error {
	ref := s.client.Doc(fmt.Sprintf("ResetRequest/%s", chargeStationId))
	_, err := ref.Set(ctx, &resetRequest{
		Type:      string(request.Type),
		Status:    string(request.Status),
		CreatedAt: request.CreatedAt,
		UpdatedAt: request.UpdatedAt,
	})
	return err
}

func (s *Store) GetResetRequest(ctx context.Context, chargeStationId string) (*store.ResetRequest, error) {
	ref := s.client.Doc(fmt.Sprintf("ResetRequest/%s", chargeStationId))
	snap, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get reset request %s: %w", chargeStationId, err)
	}
	var data resetRequest
	if err = snap.DataTo(&data); err != nil {
		return nil, fmt.Errorf("map reset request %s: %w", chargeStationId, err)
	}
	return &store.ResetRequest{
		ChargeStationId: chargeStationId,
		Type:            store.ResetType(data.Type),
		Status:          store.ResetRequestStatus(data.Status),
		CreatedAt:       data.CreatedAt,
		UpdatedAt:       data.UpdatedAt,
	}, nil
}

func (s *Store) DeleteResetRequest(ctx context.Context, chargeStationId string) error {
	ref := s.client.Doc(fmt.Sprintf("ResetRequest/%s", chargeStationId))
	_, err := ref.Delete(ctx)
	return err
}

type unlockConnectorRequest struct {
	ConnectorId int       `firestore:"connectorId"`
	Status      string    `firestore:"status"`
	CreatedAt   time.Time `firestore:"createdAt"`
	UpdatedAt   time.Time `firestore:"updatedAt"`
}

func (s *Store) SetUnlockConnectorRequest(ctx context.Context, chargeStationId string, request *store.UnlockConnectorRequest) error {
	ref := s.client.Doc(fmt.Sprintf("UnlockConnectorRequest/%s", chargeStationId))
	_, err := ref.Set(ctx, &unlockConnectorRequest{
		ConnectorId: request.ConnectorId,
		Status:      string(request.Status),
		CreatedAt:   request.CreatedAt,
		UpdatedAt:   request.UpdatedAt,
	})
	return err
}

func (s *Store) GetUnlockConnectorRequest(ctx context.Context, chargeStationId string) (*store.UnlockConnectorRequest, error) {
	ref := s.client.Doc(fmt.Sprintf("UnlockConnectorRequest/%s", chargeStationId))
	snap, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get unlock connector request %s: %w", chargeStationId, err)
	}
	var data unlockConnectorRequest
	if err = snap.DataTo(&data); err != nil {
		return nil, fmt.Errorf("map unlock connector request %s: %w", chargeStationId, err)
	}
	return &store.UnlockConnectorRequest{
		ChargeStationId: chargeStationId,
		ConnectorId:     data.ConnectorId,
		Status:          store.UnlockConnectorRequestStatus(data.Status),
		CreatedAt:       data.CreatedAt,
		UpdatedAt:       data.UpdatedAt,
	}, nil
}

func (s *Store) DeleteUnlockConnectorRequest(ctx context.Context, chargeStationId string) error {
	ref := s.client.Doc(fmt.Sprintf("UnlockConnectorRequest/%s", chargeStationId))
	_, err := ref.Delete(ctx)
	return err
}

type certificateQuery struct {
	ChargeStationId string    `firestore:"chargeStationId"`
	CertificateType *string   `firestore:"certificateType,omitempty"`
	QueryStatus     string    `firestore:"queryStatus"`
	SendAfter       time.Time `firestore:"sendAfter"`
}

func (s *Store) SetChargeStationCertificateQuery(ctx context.Context, chargeStationId string, query *store.ChargeStationCertificateQuery) error {
	ref := s.client.Doc(fmt.Sprintf("CertificateQuery/%s", chargeStationId))
	_, err := ref.Set(ctx, &certificateQuery{
		ChargeStationId: chargeStationId,
		CertificateType: query.CertificateType,
		QueryStatus:     string(query.QueryStatus),
		SendAfter:       query.SendAfter,
	})
	return err
}

func (s *Store) DeleteChargeStationCertificateQuery(ctx context.Context, chargeStationId string) error {
	ref := s.client.Doc(fmt.Sprintf("CertificateQuery/%s", chargeStationId))
	_, err := ref.Delete(ctx)
	return err
}

func (s *Store) LookupChargeStationCertificateQuery(ctx context.Context, chargeStationId string) (*store.ChargeStationCertificateQuery, error) {
	ref := s.client.Doc(fmt.Sprintf("CertificateQuery/%s", chargeStationId))
	snap, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get certificate query %s: %w", chargeStationId, err)
	}
	var data certificateQuery
	if err = snap.DataTo(&data); err != nil {
		return nil, fmt.Errorf("map certificate query %s: %w", chargeStationId, err)
	}
	return &store.ChargeStationCertificateQuery{
		ChargeStationId: chargeStationId,
		CertificateType: data.CertificateType,
		QueryStatus:     store.CertificateQueryStatus(data.QueryStatus),
		SendAfter:       data.SendAfter,
	}, nil
}

func (s *Store) ListChargeStationCertificateQueries(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationCertificateQuery, error) {
	query := s.client.Collection("CertificateQuery").
		OrderBy("chargeStationId", firestore.Asc).
		Limit(pageSize)
	if previousChargeStationId != "" {
		query = query.StartAfter(previousChargeStationId)
	}
	iter := query.Documents(ctx)
	defer iter.Stop()

	var queries []*store.ChargeStationCertificateQuery
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list certificate queries: %w", err)
		}
		var data certificateQuery
		if err = snap.DataTo(&data); err != nil {
			return nil, fmt.Errorf("map certificate query: %w", err)
		}
		queries = append(queries, &store.ChargeStationCertificateQuery{
			ChargeStationId: data.ChargeStationId,
			CertificateType: data.CertificateType,
			QueryStatus:     store.CertificateQueryStatus(data.QueryStatus),
			SendAfter:       data.SendAfter,
		})
	}
	return queries, nil
}

type certificateDeletion struct {
	ChargeStationId string    `firestore:"chargeStationId"`
	HashAlgorithm   string    `firestore:"hashAlgorithm"`
	IssuerNameHash  string    `firestore:"issuerNameHash"`
	IssuerKeyHash   string    `firestore:"issuerKeyHash"`
	SerialNumber    string    `firestore:"serialNumber"`
	DeletionStatus  string    `firestore:"deletionStatus"`
	SendAfter       time.Time `firestore:"sendAfter"`
}

func (s *Store) SetChargeStationCertificateDeletion(ctx context.Context, chargeStationId string, deletion *store.ChargeStationCertificateDeletion) error {
	ref := s.client.Doc(fmt.Sprintf("CertificateDeletion/%s", chargeStationId))
	_, err := ref.Set(ctx, &certificateDeletion{
		ChargeStationId: chargeStationId,
		HashAlgorithm:   deletion.HashAlgorithm,
		IssuerNameHash:  deletion.IssuerNameHash,
		IssuerKeyHash:   deletion.IssuerKeyHash,
		SerialNumber:    deletion.SerialNumber,
		DeletionStatus:  string(deletion.DeletionStatus),
		SendAfter:       deletion.SendAfter,
	})
	return err
}

func (s *Store) DeleteChargeStationCertificateDeletion(ctx context.Context, chargeStationId string) error {
	ref := s.client.Doc(fmt.Sprintf("CertificateDeletion/%s", chargeStationId))
	_, err := ref.Delete(ctx)
	return err
}

func (s *Store) LookupChargeStationCertificateDeletion(ctx context.Context, chargeStationId string) (*store.ChargeStationCertificateDeletion, error) {
	ref := s.client.Doc(fmt.Sprintf("CertificateDeletion/%s", chargeStationId))
	snap, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get certificate deletion %s: %w", chargeStationId, err)
	}
	var data certificateDeletion
	if err = snap.DataTo(&data); err != nil {
		return nil, fmt.Errorf("map certificate deletion %s: %w", chargeStationId, err)
	}
	return &store.ChargeStationCertificateDeletion{
		ChargeStationId: chargeStationId,
		HashAlgorithm:   data.HashAlgorithm,
		IssuerNameHash:  data.IssuerNameHash,
		IssuerKeyHash:   data.IssuerKeyHash,
		SerialNumber:    data.SerialNumber,
		DeletionStatus:  store.CertificateDeletionStatus(data.DeletionStatus),
		SendAfter:       data.SendAfter,
	}, nil
}

func (s *Store) ListChargeStationCertificateDeletions(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationCertificateDeletion, error) {
	query := s.client.Collection("CertificateDeletion").
		OrderBy("chargeStationId", firestore.Asc).
		Limit(pageSize)
	if previousChargeStationId != "" {
		query = query.StartAfter(previousChargeStationId)
	}
	iter := query.Documents(ctx)
	defer iter.Stop()

	var deletions []*store.ChargeStationCertificateDeletion
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list certificate deletions: %w", err)
		}
		var data certificateDeletion
		if err = snap.DataTo(&data); err != nil {
			return nil, fmt.Errorf("map certificate deletion: %w", err)
		}
		deletions = append(deletions, &store.ChargeStationCertificateDeletion{
			ChargeStationId: data.ChargeStationId,
			HashAlgorithm:   data.HashAlgorithm,
			IssuerNameHash:  data.IssuerNameHash,
			IssuerKeyHash:   data.IssuerKeyHash,
			SerialNumber:    data.SerialNumber,
			DeletionStatus:  store.CertificateDeletionStatus(data.DeletionStatus),
			SendAfter:       data.SendAfter,
		})
	}
	return deletions, nil
}
