package event

import (
	"context"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/machinefi/sprout-pebble-sequencer/pkg/enums"
	"github.com/machinefi/sprout-pebble-sequencer/pkg/models"
)

func init() {
	f := func() Event { return &AccountUpdated{} }
	e := f()
	registry(e.Topic(), f)
}

type AccountUpdated struct {
	Owner  common.Address
	Name   string
	Avatar string
}

func (e *AccountUpdated) Source() SourceType { return SOURCE_TYPE__BLOCKCHAIN }

func (e *AccountUpdated) Topic() string {
	return strings.Join([]string{
		"TOPIC", e.ContractID(), strings.ToUpper(e.EventName()),
	}, "__")
}

func (e *AccountUpdated) Data() any { return e }

func (e *AccountUpdated) ContractID() string { return enums.CONTRACT__PEBBLE_ACCOUNT }

func (e *AccountUpdated) EventName() string { return "Updated" }

func (e *AccountUpdated) Unmarshal(any) error { return nil }

func (e *AccountUpdated) Handle(ctx context.Context) (err error) {
	defer func() { err = WrapHandleError(err, e) }()

	m := &models.Account{
		ID:     e.Owner.String(),
		Name:   e.Name,
		Avatar: e.Avatar,
	}

	return UpsertOnConflictUpdateOthers(ctx, m, []string{"id"}, append([]*Assigner{
		{"id", m.ID},
		{"name", m.Name},
		{"avatar", m.Avatar},
		{"updated_at", time.Now()},
		{"created_at", time.Now()},
	})...)
}
