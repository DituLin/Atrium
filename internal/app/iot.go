package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/iot"
	"github.com/DituLin/Atrium/internal/iot/homeassistant"
	"github.com/DituLin/Atrium/internal/store"
)

type iotService struct {
	repo    *store.IoTRepository
	cfg     config.IoT
	bus     *events.Bus
	log     *slog.Logger
	factory func() (iot.Adapter, error)
}

// IoTReader exposes normalized observations for the family projection. Disabled
// integrations are deliberately absent rather than represented by cached data.
func (r *Runtime) IoTReader() iot.SnapshotReader {
	if r.iot == nil || !r.cfg.IoT.Enabled {
		return nil
	}
	return r.iot.repo
}

func (r *Runtime) buildIoT(ctx context.Context) error {
	repo, err := r.db.NewIoT(ctx, r.cfg.IoT)
	if err != nil {
		return err
	}
	r.iot = &iotService{repo: repo, cfg: r.cfg.IoT, bus: r.bus, log: r.log}
	return nil
}

type iotSinkFunc func(context.Context, iot.Update) error

func (f iotSinkFunc) Publish(ctx context.Context, u iot.Update) error { return f(ctx, u) }

func (s *iotService) Run(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	// Adapter construction performs token/CA I/O only inside this worker.
	generation := uint64(1)
	failed := false
	publish := iotSinkFunc(func(ctx context.Context, u iot.Update) error {
		changed, err := s.repo.Apply(ctx, u)
		// Clearing authorization hides the public cache even when persistence
		// fails. Tell screens to discard their copies before attempting a read.
		clearing := u.Replace && u.Status == iot.Disconnected && u.InstanceID == s.cfg.HomeAssistant.ID && u.Generation >= generation
		if clearing {
			s.bus.Publish(domain.TopicIoT, domain.TopicIoTClear)
		}
		if err != nil {
			failed = true
			return err
		}
		if u.Generation > generation {
			generation = u.Generation
		}
		if changed && !clearing {
			s.bus.Publish(domain.TopicIoT)
		}
		return nil
	})
	defer s.repo.Disconnect()
	defer func() {
		if recover() != nil {
			s.log.Warn("IoT worker stopped", "component", "iot", "event", "worker_failed")
		}
	}()
	factory := s.factory
	if factory == nil {
		factory = func() (iot.Adapter, error) { return homeassistant.New(s.cfg.HomeAssistant) }
	}
	adapter, err := factory()
	clear := err != nil
	if err == nil {
		err = adapter.Run(ctx, publish)
		clear = errors.Is(err, homeassistant.ErrUnauthorized)
	}
	// A failed sink is terminal. Never continue processing a connection whose
	// persistence failed. The reader is disconnected even if SQL stays unavailable.
	if !failed {
		finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if e := publish(finalCtx, iot.Update{InstanceID: s.cfg.HomeAssistant.ID, Generation: generation, Status: iot.Disconnected, Replace: clear}); e != nil {
			err = e
		}
	}
	if err != nil && ctx.Err() == nil {
		s.log.Warn("IoT worker stopped", "component", "iot", "event", "worker_failed")
	}
}
