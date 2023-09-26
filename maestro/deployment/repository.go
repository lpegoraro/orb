package deployment

import (
	"context"
	"fmt"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // required for SQL access
	"github.com/orb-community/orb/pkg/errors"
	"go.uber.org/zap"
	"time"
)

type Repository interface {
	FetchAll(ctx context.Context) ([]Deployment, error)
	Add(ctx context.Context, deployment *Deployment) (*Deployment, error)
	Update(ctx context.Context, deployment *Deployment) (*Deployment, error)
	UpdateStatus(ctx context.Context, ownerID string, sinkId string, status string, errorMessage string) error
	Remove(ctx context.Context, ownerId string, sinkId string) error
	FindByOwnerAndSink(ctx context.Context, ownerId string, sinkId string) (*Deployment, error)
	FindByCollectorName(ctx context.Context, collectorName string) (*Deployment, error)
}

var _ Repository = (*repositoryService)(nil)

func NewRepositoryService(db *sqlx.DB, logger *zap.Logger) Repository {
	namedLogger := logger.Named("deployment-repository")
	return &repositoryService{db: db, logger: namedLogger}
}

type repositoryService struct {
	logger *zap.Logger
	db     *sqlx.DB
}

func (r *repositoryService) FetchAll(ctx context.Context) ([]Deployment, error) {
	tx := r.db.MustBeginTx(ctx, nil)
	var deployments []Deployment
	err := tx.SelectContext(ctx, &deployments, "SELECT * FROM deployments", nil)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	err = tx.Commit()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	r.logger.Debug("fetched all deployments", zap.Int("count", len(deployments)))
	return deployments, nil
}

func (r *repositoryService) Add(ctx context.Context, deployment *Deployment) (*Deployment, error) {
	tx := r.db.MustBeginTx(ctx, nil)
	cmd, err := tx.NamedExecContext(ctx,
		`INSERT INTO deployments (id, owner_id, sink_id, backend, config, last_status, last_status_update, last_error_message, 
				last_error_time, collector_name, last_collector_deploy_time, last_collector_stop_time) 
				VALUES (:id, :owner_id, :sink_id, :backend, :config, :last_status, :last_status_update, :last_error_message, 
				        :last_error_time, :collector_name, :last_collector_deploy_time, :last_collector_stop_time)`,
		deployment)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	newId, err := cmd.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	deployment.Id = fmt.Sprintf("%d", newId)
	r.logger.Debug("added deployment", zap.String("owner-id", deployment.OwnerID), zap.String("sink-id", deployment.SinkID))
	return deployment, tx.Commit()
}

func (r *repositoryService) Update(ctx context.Context, deployment *Deployment) (*Deployment, error) {
	tx := r.db.MustBeginTx(ctx, nil)
	_, err := tx.NamedExecContext(ctx,
		`UPDATE deployments 
				SET 
                       owner_id = :owner_id,
                       sink_id = :sink_id,
                       backend = :backend,
                       config = :config,
                       last_status = :last_status, 
                       last_status_update = :last_status_update, 
                       last_error_message = :last_error_message,
					   last_error_time = :last_error_time, 
					   collector_name = :collector_name, 
					   last_collector_deploy_time = :last_collector_deploy_time, 
					   last_collector_stop_time = :last_collector_stop_time 
				WHERE id = :id`,
		deployment)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	r.logger.Info("update deployment", zap.String("owner-id", deployment.OwnerID), zap.String("sink-id", deployment.SinkID))
	return deployment, tx.Commit()
}

func (r *repositoryService) UpdateStatus(ctx context.Context, ownerID string, sinkId string, status string, errorMessage string) error {
	tx := r.db.MustBeginTx(ctx, nil)
	now := time.Now()
	_, err := tx.ExecContext(ctx,
		`UPDATE deployments 
				SET 
					   last_status = $1, 
					   last_status_update = $2, 
					   last_error_message = $3,
					   last_error_time = $4
				WHERE owner_id = $5 AND sink_id = $6`,
		status, now, errorMessage, now, ownerID, sinkId)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	r.logger.Debug("update deployment", zap.String("owner-id", ownerID), zap.String("sink-id", sinkId))
	return tx.Commit()
}

func (r *repositoryService) Remove(ctx context.Context, ownerId string, sinkId string) error {
	tx := r.db.MustBeginTx(ctx, nil)
	tx.MustExecContext(ctx, "DELETE FROM deployments WHERE owner_id = $1 AND sink_id = $2", ownerId, sinkId)
	err := tx.Commit()
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}

func (r *repositoryService) FindByOwnerAndSink(ctx context.Context, ownerId string, sinkId string) (*Deployment, error) {
	tx := r.db.MustBeginTx(ctx, nil)
	var rows []Deployment
	err := tx.SelectContext(ctx, &rows, "SELECT * FROM deployments WHERE owner_id = :owner_id AND sink_id = :sink_id",
		map[string]interface{}{"owner_id": ownerId, "sink_id": sinkId})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	err = tx.Commit()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New(fmt.Sprintf("not found deployment for owner-id: %s and sink-id: %s", ownerId, sinkId))
	}
	deployment := &rows[0]

	return deployment, nil
}

func (r *repositoryService) FindByCollectorName(ctx context.Context, collectorName string) (*Deployment, error) {
	tx := r.db.MustBeginTx(ctx, nil)
	var rows []Deployment
	err := tx.SelectContext(ctx, &rows, "SELECT * FROM deployments WHERE collector_name = :collector_name",
		map[string]interface{}{"collector_name": collectorName})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	err = tx.Commit()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New(fmt.Sprintf("not found deployment for collector name: %s", collectorName))
	}
	deployment := &rows[0]

	return deployment, nil
}
