package exchange

import (
	"context"

	multierror "github.com/hashicorp/go-multierror"
	msfolderdelta "github.com/microsoftgraph/msgraph-sdk-go/users"
	"github.com/pkg/errors"

	"github.com/alcionai/corso/src/internal/connector/graph"
	"github.com/alcionai/corso/src/internal/connector/support"
	"github.com/alcionai/corso/src/pkg/path"
)

var _ graph.ContainerResolver = &mailFolderCache{}

// mailFolderCache struct used to improve lookup of directories within exchange.Mail
// cache map of cachedContainers where the  key =  M365ID
// nameLookup map: Key: DisplayName Value: ID
type mailFolderCache struct {
	*containerResolver
	gs     graph.Servicer
	userID string
}

// populateMailRoot manually fetches directories that are not returned during Graph for msgraph-sdk-go v. 40+
// rootFolderAlias is the top-level directory for exchange.Mail.
// DefaultMailFolder is the traditional "Inbox" for exchange.Mail
// Action ensures that cache will stop at appropriate level.
// @error iff the struct is not properly instantiated
func (mc *mailFolderCache) populateMailRoot(
	ctx context.Context,
) error {
	wantedOpts := []string{"displayName", "parentFolderId"}

	opts, err := optionsForMailFoldersItem(wantedOpts)
	if err != nil {
		return errors.Wrapf(err, "getting options for mail folders %v", wantedOpts)
	}

	for _, fldr := range []string{rootFolderAlias, DefaultMailFolder} {
		var directory string

		f, err := mc.
			gs.
			Client().
			UsersById(mc.userID).
			MailFoldersById(fldr).
			Get(ctx, opts)
		if err != nil {
			return errors.Wrap(err, "fetching root folder"+support.ConnectorStackErrorTrace(err))
		}

		if fldr == DefaultMailFolder {
			directory = DefaultMailFolder
		}

		temp := graph.NewCacheFolder(f, path.Builder{}.Append(directory))

		if err := mc.addFolder(temp); err != nil {
			return errors.Wrap(err, "initializing mail resolver")
		}
	}

	return nil
}

// Populate utility function for populating the mailFolderCache.
// Number of Graph Queries: 1.
// @param baseID: M365ID of the base of the exchange.Mail.Folder
// @param baseContainerPath: the set of folder elements that make up the path
// for the base container in the cache.
func (mc *mailFolderCache) Populate(
	ctx context.Context,
	baseID string,
	baseContainerPath ...string,
) error {
	if err := mc.init(ctx); err != nil {
		return err
	}

	// Even though this uses the `Delta` query, we do no store or re-use
	// the delta-link tokens like with other queries.  The goal is always
	// to retrieve the complete history of folders.
	query := mc.
		gs.
		Client().
		UsersById(mc.userID).
		MailFolders().
		Delta()

	var errs *multierror.Error

	for {
		resp, err := query.Get(ctx, nil)
		if err != nil {
			return errors.Wrap(err, support.ConnectorStackErrorTrace(err))
		}

		for _, f := range resp.GetValue() {
			temp := graph.NewCacheFolder(f, nil)

			// Use addFolder instead of AddToCache to be conservative about path
			// population. The fetch order of the folders could cause failures while
			// trying to resolve paths, so put it off until we've gotten all folders.
			if err := mc.addFolder(temp); err != nil {
				errs = multierror.Append(errs, errors.Wrap(err, "delta fetch"))
				continue
			}
		}

		link := resp.GetOdataNextLink()
		if link == nil {
			break
		}

		query = msfolderdelta.NewItemMailFoldersDeltaRequestBuilder(*link, mc.gs.Adapter())
	}

	if err := mc.populatePaths(ctx); err != nil {
		errs = multierror.Append(errs, errors.Wrap(err, "mail resolver"))
	}

	return errs.ErrorOrNil()
}

// init ensures that the structure's fields are initialized.
// Fields Initialized when cache == nil:
// [mc.cache]
func (mc *mailFolderCache) init(
	ctx context.Context,
) error {
	if mc.containerResolver == nil {
		mc.containerResolver = newContainerResolver()
	}

	return mc.populateMailRoot(ctx)
}
