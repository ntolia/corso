package sharepoint

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"github.com/alcionai/corso/src/internal/connector/graph"
	"github.com/alcionai/corso/src/internal/connector/onedrive"
	"github.com/alcionai/corso/src/internal/connector/support"
	"github.com/alcionai/corso/src/internal/data"
	"github.com/alcionai/corso/src/internal/observe"
	"github.com/alcionai/corso/src/pkg/control"
	"github.com/alcionai/corso/src/pkg/logger"
	"github.com/alcionai/corso/src/pkg/path"
	"github.com/alcionai/corso/src/pkg/selectors"
)

type statusUpdater interface {
	UpdateStatus(status *support.ConnectorOperationStatus)
}

// DataCollections returns a set of DataCollection which represents the SharePoint data
// for the specified user
func DataCollections(
	ctx context.Context,
	selector selectors.Selector,
	siteIDs []string,
	tenantID string,
	serv graph.Servicer,
	su statusUpdater,
	ctrlOpts control.Options,
) ([]data.Collection, error) {
	b, err := selector.ToSharePointBackup()
	if err != nil {
		return nil, errors.Wrap(err, "sharePointDataCollection: parsing selector")
	}

	var (
		scopes      = b.DiscreteScopes(siteIDs)
		collections = []data.Collection{}
		errs        error
	)

	for _, scope := range scopes {
		// due to DiscreteScopes(siteIDs), each range should only contain one site.
		for _, site := range scope.Get(selectors.SharePointSite) {
			foldersComplete, closer := observe.MessageWithCompletion(fmt.Sprintf(
				"∙ %s - %s:",
				scope.Category().PathType(), site))
			defer closer()
			defer close(foldersComplete)

			switch scope.Category().PathType() {
			// TODO path.ListCategory: PR
			// collect Lists
			// done?
			case path.ListsCategory:
				return nil, fmt.Errorf("sharePoint list collections not supported")

			case path.LibrariesCategory:
				spcs, err := collectLibraries(
					ctx,
					serv,
					tenantID,
					site,
					scope,
					su,
					ctrlOpts)
				if err != nil {
					return nil, support.WrapAndAppend(site, err, errs)
				}

				collections = append(collections, spcs...)
			}

			foldersComplete <- struct{}{}
		}
	}

	return collections, errs
}

// collectLibraries constructs a onedrive Collections struct and Get()s
// all the drives associated with the site.
func collectLibraries(
	ctx context.Context,
	serv graph.Servicer,
	tenantID, siteID string,
	scope selectors.SharePointScope,
	updater statusUpdater,
	ctrlOpts control.Options,
) ([]data.Collection, error) {
	var (
		collections = []data.Collection{}
		errs        error
	)

	logger.Ctx(ctx).With("site", siteID).Debug("Creating SharePoint Library collections")

	colls := onedrive.NewCollections(
		tenantID,
		siteID,
		onedrive.SharePointSource,
		folderMatcher{scope},
		serv,
		updater.UpdateStatus,
		ctrlOpts)

	odcs, err := colls.Get(ctx)
	if err != nil {
		return nil, support.WrapAndAppend(siteID, err, errs)
	}

	return append(collections, odcs...), errs
}

type folderMatcher struct {
	scope selectors.SharePointScope
}

func (fm folderMatcher) IsAny() bool {
	return fm.scope.IsAny(selectors.SharePointLibrary)
}

func (fm folderMatcher) Matches(dir string) bool {
	return fm.scope.Matches(selectors.SharePointLibrary, dir)
}
