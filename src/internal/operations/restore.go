package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	"github.com/alcionai/corso/src/internal/common"
	"github.com/alcionai/corso/src/internal/connector/support"
	"github.com/alcionai/corso/src/internal/data"
	D "github.com/alcionai/corso/src/internal/diagnostics"
	"github.com/alcionai/corso/src/internal/events"
	"github.com/alcionai/corso/src/internal/kopia"
	"github.com/alcionai/corso/src/internal/model"
	"github.com/alcionai/corso/src/internal/observe"
	"github.com/alcionai/corso/src/internal/stats"
	"github.com/alcionai/corso/src/internal/streamstore"
	"github.com/alcionai/corso/src/pkg/account"
	"github.com/alcionai/corso/src/pkg/backup/details"
	"github.com/alcionai/corso/src/pkg/control"
	"github.com/alcionai/corso/src/pkg/logger"
	"github.com/alcionai/corso/src/pkg/path"
	"github.com/alcionai/corso/src/pkg/selectors"
	"github.com/alcionai/corso/src/pkg/store"
)

// RestoreOperation wraps an operation with restore-specific props.
type RestoreOperation struct {
	operation

	BackupID    model.StableID             `json:"backupID"`
	Results     RestoreResults             `json:"results"`
	Selectors   selectors.Selector         `json:"selectors"`
	Destination control.RestoreDestination `json:"destination"`
	Version     string                     `json:"version"`

	account account.Account
}

// RestoreResults aggregate the details of the results of the operation.
type RestoreResults struct {
	stats.Errs
	stats.ReadWrites
	stats.StartAndEndTime
}

// NewRestoreOperation constructs and validates a restore operation.
func NewRestoreOperation(
	ctx context.Context,
	opts control.Options,
	kw *kopia.Wrapper,
	sw *store.Wrapper,
	acct account.Account,
	backupID model.StableID,
	sel selectors.Selector,
	dest control.RestoreDestination,
	bus events.Eventer,
) (RestoreOperation, error) {
	op := RestoreOperation{
		operation:   newOperation(opts, bus, kw, sw),
		BackupID:    backupID,
		Selectors:   sel,
		Destination: dest,
		Version:     "v0",
		account:     acct,
	}
	if err := op.validate(); err != nil {
		return RestoreOperation{}, err
	}

	return op, nil
}

func (op RestoreOperation) validate() error {
	return op.operation.validate()
}

// aggregates stats from the restore.Run().
// primarily used so that the defer can take in a
// pointer wrapping the values, while those values
// get populated asynchronously.
type restoreStats struct {
	cs                []data.Collection
	gc                *support.ConnectorOperationStatus
	bytesRead         *stats.ByteCounter
	resourceCount     int
	started           bool
	readErr, writeErr error

	// a transient value only used to pair up start-end events.
	restoreID string
}

// Run begins a synchronous restore operation.
func (op *RestoreOperation) Run(ctx context.Context) (restoreDetails *details.Details, err error) {
	ctx, end := D.Span(ctx, "operations:restore:run")
	defer end()

	var (
		opStats = restoreStats{
			bytesRead: &stats.ByteCounter{},
			restoreID: uuid.NewString(),
		}
		startTime = time.Now()
	)

	defer func() {
		// wait for the progress display to clean up
		observe.Complete()

		err = op.persistResults(ctx, startTime, &opStats)
		if err != nil {
			return
		}
	}()

	dID, bup, err := op.store.GetDetailsIDFromBackupID(ctx, op.BackupID)
	if err != nil {
		err = errors.Wrap(err, "getting backup details ID for restore")
		opStats.readErr = err

		return nil, err
	}

	deets, err := streamstore.New(
		op.kopia,
		op.account.ID(),
		op.Selectors.PathService(),
	).ReadBackupDetails(ctx, dID)
	if err != nil {
		err = errors.Wrap(err, "getting backup details data for restore")
		opStats.readErr = err

		return nil, err
	}

	op.bus.Event(
		ctx,
		events.RestoreStart,
		map[string]any{
			events.StartTime:        startTime,
			events.BackupID:         op.BackupID,
			events.BackupCreateTime: bup.CreationTime,
			events.RestoreID:        opStats.restoreID,
			// TODO: restore options,
		},
	)

	paths, err := formatDetailsForRestoration(ctx, op.Selectors, deets)
	if err != nil {
		opStats.readErr = err
		return nil, err
	}

	observe.Message(fmt.Sprintf("Discovered %d items in backup %s to restore", len(paths), op.BackupID))

	kopiaComplete, closer := observe.MessageWithCompletion("Enumerating items in repository:")
	defer closer()
	defer close(kopiaComplete)

	dcs, err := op.kopia.RestoreMultipleItems(ctx, bup.SnapshotID, paths, opStats.bytesRead)
	if err != nil {
		err = errors.Wrap(err, "retrieving service data")
		opStats.readErr = err

		return nil, err
	}
	kopiaComplete <- struct{}{}

	opStats.cs = dcs
	opStats.resourceCount = len(data.ResourceOwnerSet(dcs))

	gc, err := connectToM365(ctx, op.Selectors, op.account)
	if err != nil {
		opStats.readErr = errors.Wrap(err, "connecting to M365")
		return nil, opStats.readErr
	}

	restoreComplete, closer := observe.MessageWithCompletion("Restoring data:")
	defer closer()
	defer close(restoreComplete)

	restoreDetails, err = gc.RestoreDataCollections(ctx, op.Selectors, op.Destination, dcs)
	if err != nil {
		err = errors.Wrap(err, "restoring service data")
		opStats.writeErr = err

		return nil, err
	}
	restoreComplete <- struct{}{}

	opStats.started = true
	opStats.gc = gc.AwaitStatus()

	logger.Ctx(ctx).Debug(gc.PrintableStatus())

	return restoreDetails, nil
}

// persists details and statistics about the restore operation.
func (op *RestoreOperation) persistResults(
	ctx context.Context,
	started time.Time,
	opStats *restoreStats,
) error {
	op.Results.StartedAt = started
	op.Results.CompletedAt = time.Now()

	op.Status = Completed

	if !opStats.started {
		op.Status = Failed

		return multierror.Append(
			errors.New("errors prevented the operation from processing"),
			opStats.readErr,
			opStats.writeErr)
	}

	if opStats.readErr == nil && opStats.writeErr == nil && opStats.gc.Successful == 0 {
		op.Status = NoData
	}

	op.Results.ReadErrors = opStats.readErr
	op.Results.WriteErrors = opStats.writeErr

	op.Results.BytesRead = opStats.bytesRead.NumBytes
	op.Results.ItemsRead = len(opStats.cs) // TODO: file count, not collection count
	op.Results.ItemsWritten = opStats.gc.Successful
	op.Results.ResourceOwners = opStats.resourceCount

	dur := op.Results.CompletedAt.Sub(op.Results.StartedAt)

	op.bus.Event(
		ctx,
		events.RestoreEnd,
		map[string]any{
			events.BackupID:      op.BackupID,
			events.DataRetrieved: op.Results.BytesRead,
			events.Duration:      dur,
			events.EndTime:       common.FormatTime(op.Results.CompletedAt),
			events.ItemsRead:     op.Results.ItemsRead,
			events.ItemsWritten:  op.Results.ItemsWritten,
			events.Resources:     op.Results.ResourceOwners,
			events.RestoreID:     opStats.restoreID,
			events.Service:       op.Selectors.Service.String(),
			events.StartTime:     common.FormatTime(op.Results.StartedAt),
			events.Status:        op.Status.String(),
		},
	)

	return nil
}

// formatDetailsForRestoration reduces the provided detail entries according to the
// selector specifications.
func formatDetailsForRestoration(
	ctx context.Context,
	sel selectors.Selector,
	deets *details.Details,
) ([]path.Path, error) {
	fds, err := sel.Reduce(ctx, deets)
	if err != nil {
		return nil, err
	}

	var (
		errs     *multierror.Error
		fdsPaths = fds.Paths()
		paths    = make([]path.Path, len(fdsPaths))
	)

	for i := range fdsPaths {
		p, err := path.FromDataLayerPath(fdsPaths[i], true)
		if err != nil {
			errs = multierror.Append(
				errs,
				errors.Wrap(err, "parsing details entry path"),
			)

			continue
		}

		paths[i] = p
	}

	return paths, nil
}
