package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"

	"github.com/alcionai/corso/src/internal/events"
	"github.com/alcionai/corso/src/internal/kopia"
	"github.com/alcionai/corso/src/internal/model"
	"github.com/alcionai/corso/src/internal/observe"
	"github.com/alcionai/corso/src/internal/operations"
	"github.com/alcionai/corso/src/internal/streamstore"
	"github.com/alcionai/corso/src/pkg/account"
	"github.com/alcionai/corso/src/pkg/backup"
	"github.com/alcionai/corso/src/pkg/backup/details"
	"github.com/alcionai/corso/src/pkg/control"
	"github.com/alcionai/corso/src/pkg/logger"
	"github.com/alcionai/corso/src/pkg/selectors"
	"github.com/alcionai/corso/src/pkg/storage"
	"github.com/alcionai/corso/src/pkg/store"
)

var ErrorRepoAlreadyExists = errors.New("a repository was already initialized with that configuration")

// BackupGetter deals with retrieving metadata about backups from the
// repository.
type BackupGetter interface {
	Backup(ctx context.Context, id model.StableID) (*backup.Backup, error)
	Backups(ctx context.Context, ids []model.StableID) ([]*backup.Backup, error)
	BackupsByTag(ctx context.Context, fs ...store.FilterOption) ([]*backup.Backup, error)
	BackupDetails(
		ctx context.Context,
		backupID string,
	) (*details.Details, *backup.Backup, error)
}

type Repository interface {
	GetID() string
	Close(context.Context) error
	NewBackup(
		ctx context.Context,
		self selectors.Selector,
	) (operations.BackupOperation, error)
	NewRestore(
		ctx context.Context,
		backupID string,
		sel selectors.Selector,
		dest control.RestoreDestination,
	) (operations.RestoreOperation, error)
	DeleteBackup(ctx context.Context, id model.StableID) error
	BackupGetter
}

// Repository contains storage provider information.
type repository struct {
	ID        string
	CreatedAt time.Time
	Version   string // in case of future breaking changes

	Account account.Account // the user's m365 account connection details
	Storage storage.Storage // the storage provider details and configuration
	Opts    control.Options

	Bus        events.Eventer
	dataLayer  *kopia.Wrapper
	modelStore *kopia.ModelStore
}

func (r repository) GetID() string {
	return r.ID
}

// Initialize will:
//   - validate the m365 account & secrets
//   - connect to the m365 account to ensure communication capability
//   - validate the provider config & secrets
//   - initialize the kopia repo with the provider
//   - store the configuration details
//   - connect to the provider
//   - return the connected repository
func Initialize(
	ctx context.Context,
	acct account.Account,
	s storage.Storage,
	opts control.Options,
) (Repository, error) {
	kopiaRef := kopia.NewConn(s)
	if err := kopiaRef.Initialize(ctx); err != nil {
		// replace common internal errors so that sdk users can check results with errors.Is()
		if kopia.IsRepoAlreadyExistsError(err) {
			return nil, ErrorRepoAlreadyExists
		}

		return nil, err
	}
	// kopiaRef comes with a count of 1 and NewWrapper/NewModelStore bumps it again so safe
	// to close here.
	defer kopiaRef.Close(ctx)

	w, err := kopia.NewWrapper(kopiaRef)
	if err != nil {
		return nil, err
	}

	ms, err := kopia.NewModelStore(kopiaRef)
	if err != nil {
		return nil, err
	}

	bus, err := events.NewBus(ctx, s, acct.ID(), opts)
	if err != nil {
		return nil, err
	}

	repoID := newRepoID(s)
	bus.SetRepoID(repoID)

	r := &repository{
		ID:         repoID,
		Version:    "v1",
		Account:    acct,
		Storage:    s,
		Bus:        bus,
		dataLayer:  w,
		modelStore: ms,
	}

	if err := newRepoModel(ctx, ms, r.ID); err != nil {
		return nil, errors.New("setting up repository")
	}

	r.Bus.Event(ctx, events.RepoInit, nil)

	return r, nil
}

// Connect will:
//   - validate the m365 account details
//   - connect to the m365 account to ensure communication capability
//   - connect to the provider storage
//   - return the connected repository
func Connect(
	ctx context.Context,
	acct account.Account,
	s storage.Storage,
	opts control.Options,
) (Repository, error) {
	complete, closer := observe.MessageWithCompletion("Connecting to repository:")
	defer closer()
	defer close(complete)

	kopiaRef := kopia.NewConn(s)
	if err := kopiaRef.Connect(ctx); err != nil {
		return nil, err
	}
	// kopiaRef comes with a count of 1 and NewWrapper/NewModelStore bumps it again so safe
	// to close here.
	defer kopiaRef.Close(ctx)

	w, err := kopia.NewWrapper(kopiaRef)
	if err != nil {
		return nil, err
	}

	ms, err := kopia.NewModelStore(kopiaRef)
	if err != nil {
		return nil, err
	}

	bus, err := events.NewBus(ctx, s, acct.ID(), opts)
	if err != nil {
		return nil, err
	}

	rm, err := getRepoModel(ctx, ms)
	if err != nil {
		return nil, errors.New("retrieving repo info")
	}

	bus.SetRepoID(string(rm.ID))

	complete <- struct{}{}

	// todo: ID and CreatedAt should get retrieved from a stored kopia config.
	return &repository{
		ID:         string(rm.ID),
		Version:    "v1",
		Account:    acct,
		Storage:    s,
		Bus:        bus,
		dataLayer:  w,
		modelStore: ms,
	}, nil
}

func (r *repository) Close(ctx context.Context) error {
	if err := r.Bus.Close(); err != nil {
		logger.Ctx(ctx).Debugw("closing the event bus", "err", err)
	}

	if r.dataLayer != nil {
		if err := r.dataLayer.Close(ctx); err != nil {
			logger.Ctx(ctx).Debugw("closing Datalayer", "err", err)
		}

		r.dataLayer = nil
	}

	if r.modelStore != nil {
		if err := r.modelStore.Close(ctx); err != nil {
			logger.Ctx(ctx).Debugw("closing modelStore", "err", err)
		}

		r.modelStore = nil
	}

	return nil
}

// NewBackup generates a BackupOperation runner.
func (r repository) NewBackup(
	ctx context.Context,
	selector selectors.Selector,
) (operations.BackupOperation, error) {
	return operations.NewBackupOperation(
		ctx,
		r.Opts,
		r.dataLayer,
		store.NewKopiaStore(r.modelStore),
		r.Account,
		selector,
		r.Bus)
}

// NewRestore generates a restoreOperation runner.
func (r repository) NewRestore(
	ctx context.Context,
	backupID string,
	sel selectors.Selector,
	dest control.RestoreDestination,
) (operations.RestoreOperation, error) {
	return operations.NewRestoreOperation(
		ctx,
		r.Opts,
		r.dataLayer,
		store.NewKopiaStore(r.modelStore),
		r.Account,
		model.StableID(backupID),
		sel,
		dest,
		r.Bus)
}

// backups lists a backup by id
func (r repository) Backup(ctx context.Context, id model.StableID) (*backup.Backup, error) {
	sw := store.NewKopiaStore(r.modelStore)
	return sw.GetBackup(ctx, id)
}

// BackupsByID lists backups by ID. Returns as many backups as possible with
// errors for the backups it was unable to retrieve.
func (r repository) Backups(ctx context.Context, ids []model.StableID) ([]*backup.Backup, error) {
	var (
		errs *multierror.Error
		bups []*backup.Backup
		sw   = store.NewKopiaStore(r.modelStore)
	)

	for _, id := range ids {
		b, err := sw.GetBackup(ctx, id)
		if err != nil {
			errs = multierror.Append(errs, err)
		}

		bups = append(bups, b)
	}

	return bups, errs.ErrorOrNil()
}

// backups lists backups in a repository
func (r repository) BackupsByTag(ctx context.Context, fs ...store.FilterOption) ([]*backup.Backup, error) {
	sw := store.NewKopiaStore(r.modelStore)
	return sw.GetBackups(ctx, fs...)
}

// BackupDetails returns the specified backup details object
func (r repository) BackupDetails(ctx context.Context, backupID string) (*details.Details, *backup.Backup, error) {
	sw := store.NewKopiaStore(r.modelStore)

	dID, b, err := sw.GetDetailsIDFromBackupID(ctx, model.StableID(backupID))
	if err != nil {
		return nil, nil, err
	}

	deets, err := streamstore.New(
		r.dataLayer,
		r.Account.ID(),
		b.Selectors.PathService()).ReadBackupDetails(ctx, dID)
	if err != nil {
		return nil, nil, err
	}

	return deets, b, nil
}

// DeleteBackup removes the backup from both the model store and the backup storage.
func (r repository) DeleteBackup(ctx context.Context, id model.StableID) error {
	bu, err := r.Backup(ctx, id)
	if err != nil {
		return err
	}

	if err := r.dataLayer.DeleteSnapshot(ctx, bu.SnapshotID); err != nil {
		return err
	}

	sw := store.NewKopiaStore(r.modelStore)

	return sw.DeleteBackup(ctx, id)
}

// ---------------------------------------------------------------------------
// Repository ID Model
// ---------------------------------------------------------------------------

// repositoryModel identifies the current repository
type repositoryModel struct {
	model.BaseModel
}

// should only be called on init.
func newRepoModel(ctx context.Context, ms *kopia.ModelStore, repoID string) error {
	rm := repositoryModel{
		BaseModel: model.BaseModel{
			ID: model.StableID(repoID),
		},
	}

	return ms.Put(ctx, model.RepositorySchema, &rm)
}

// retrieves the repository info
func getRepoModel(ctx context.Context, ms *kopia.ModelStore) (*repositoryModel, error) {
	bms, err := ms.GetIDsForType(ctx, model.RepositorySchema, nil)
	if err != nil {
		return nil, err
	}

	rm := &repositoryModel{}
	if len(bms) == 0 {
		return rm, nil
	}

	rm.BaseModel = *bms[0]

	return rm, nil
}

// newRepoID generates a new unique repository id hash.
// Repo IDs should only be generated once per repository,
// and must be stored after that.
func newRepoID(s storage.Storage) string {
	return uuid.NewString()
}
