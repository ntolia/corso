package backup

import (
	"context"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/alcionai/corso/src/cli/config"
	"github.com/alcionai/corso/src/cli/options"
	. "github.com/alcionai/corso/src/cli/print"
	"github.com/alcionai/corso/src/cli/utils"
	"github.com/alcionai/corso/src/internal/connector"
	"github.com/alcionai/corso/src/internal/kopia"
	"github.com/alcionai/corso/src/internal/model"
	"github.com/alcionai/corso/src/pkg/backup"
	"github.com/alcionai/corso/src/pkg/backup/details"
	"github.com/alcionai/corso/src/pkg/path"
	"github.com/alcionai/corso/src/pkg/repository"
	"github.com/alcionai/corso/src/pkg/selectors"
	"github.com/alcionai/corso/src/pkg/store"
)

// ------------------------------------------------------------------------------------------------
// setup and globals
// ------------------------------------------------------------------------------------------------

var (
	libraryItems []string
	libraryPaths []string
	site         []string
	weburl       []string

	sharepointData []string
)

const (
	dataLibraries = "libraries"
)

const (
	sharePointServiceCommand                 = "sharepoint"
	sharePointServiceCommandCreateUseSuffix  = "--site <siteId> | '" + utils.Wildcard + "'"
	sharePointServiceCommandDeleteUseSuffix  = "--backup <backupId>"
	sharePointServiceCommandDetailsUseSuffix = "--backup <backupId>"
)

const (
	sharePointServiceCommandCreateExamples = `# Backup SharePoint data for <site>
corso backup create sharepoint --site <site_id>

# Backup SharePoint for Alice and Bob
corso backup create sharepoint --site <site_id_1>,<site_id_2>

# TODO: Site IDs may contain commas.  We'll need to warn the site about escaping them.

# Backup all SharePoint data for all sites
corso backup create sharepoint --site '*'`

	sharePointServiceCommandDeleteExamples = `# Delete SharePoint backup with ID 1234abcd-12ab-cd34-56de-1234abcd
corso backup delete sharepoint --backup 1234abcd-12ab-cd34-56de-1234abcd`

	sharePointServiceCommandDetailsExamples = `# Explore <site>'s files from backup 1234abcd-12ab-cd34-56de-1234abcd

corso backup details sharepoint --backup 1234abcd-12ab-cd34-56de-1234abcd --site <site_id>`
)

// called by backup.go to map subcommands to provider-specific handling.
func addSharePointCommands(cmd *cobra.Command) *cobra.Command {
	var (
		c  *cobra.Command
		fs *pflag.FlagSet
	)

	switch cmd.Use {
	case createCommand:
		c, fs = utils.AddCommand(cmd, sharePointCreateCmd(), utils.HideCommand())

		c.Use = c.Use + " " + sharePointServiceCommandCreateUseSuffix
		c.Example = sharePointServiceCommandCreateExamples

		fs.StringArrayVar(&site,
			utils.SiteFN, nil,
			"Backup SharePoint data by site ID; accepts '"+utils.Wildcard+"' to select all sites.")

		fs.StringSliceVar(&weburl,
			utils.WebURLFN, nil,
			"Restore data by site webURL; accepts '"+utils.Wildcard+"' to select all sites.")

		// TODO: implement
		fs.StringSliceVar(
			&sharepointData,
			utils.DataFN, nil,
			"Select one or more types of data to backup: "+dataLibraries+".")
		options.AddOperationFlags(c)

	case listCommand:
		c, fs = utils.AddCommand(cmd, sharePointListCmd(), utils.HideCommand())

		fs.StringVar(&backupID,
			utils.BackupFN, "",
			"ID of the backup to retrieve.")

	case detailsCommand:
		c, fs = utils.AddCommand(cmd, sharePointDetailsCmd())

		c.Use = c.Use + " " + sharePointServiceCommandDetailsUseSuffix
		c.Example = sharePointServiceCommandDetailsExamples

		fs.StringVar(&backupID,
			utils.BackupFN, "",
			"ID of the backup to retrieve.")
		cobra.CheckErr(c.MarkFlagRequired(utils.BackupFN))

		// sharepoint hierarchy flags

		fs.StringSliceVar(
			&libraryPaths,
			utils.LibraryFN, nil,
			"Select backup details by Library name.")

		fs.StringSliceVar(
			&libraryItems,
			utils.LibraryItemFN, nil,
			"Select backup details by library item name or ID.")

		fs.StringArrayVar(&site,
			utils.SiteFN, nil,
			"Backup SharePoint data by site ID; accepts '"+utils.Wildcard+"' to select all sites.")

		fs.StringSliceVar(&weburl,
			utils.WebURLFN, nil,
			"Restore data by site webURL; accepts '"+utils.Wildcard+"' to select all sites.")

		// info flags

		// fs.StringVar(
		// 	&fileCreatedAfter,
		// 	utils.FileCreatedAfterFN, "",
		// 	"Select backup details for items created after this datetime.")

	case deleteCommand:
		c, fs = utils.AddCommand(cmd, sharePointDeleteCmd(), utils.HideCommand())

		c.Use = c.Use + " " + sharePointServiceCommandDeleteUseSuffix
		c.Example = sharePointServiceCommandDeleteExamples

		fs.StringVar(&backupID,
			utils.BackupFN, "",
			"ID of the backup to delete. (required)")
		cobra.CheckErr(c.MarkFlagRequired(utils.BackupFN))
	}

	return c
}

// ------------------------------------------------------------------------------------------------
// backup create
// ------------------------------------------------------------------------------------------------

// `corso backup create sharepoint [<flag>...]`
func sharePointCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     sharePointServiceCommand,
		Short:   "Backup M365 SharePoint service data",
		RunE:    createSharePointCmd,
		Args:    cobra.NoArgs,
		Example: sharePointServiceCommandCreateExamples,
	}
}

// processes an sharepoint service backup.
func createSharePointCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if utils.HasNoFlagsAndShownHelp(cmd) {
		return nil
	}

	if err := validateSharePointBackupCreateFlags(site, weburl); err != nil {
		return err
	}

	s, acct, err := config.GetStorageAndAccount(ctx, true, nil)
	if err != nil {
		return Only(ctx, err)
	}

	r, err := repository.Connect(ctx, acct, s, options.Control())
	if err != nil {
		return Only(ctx, errors.Wrapf(err, "Failed to connect to the %s repository", s.Provider))
	}

	defer utils.CloseRepo(ctx, r)

	gc, err := connector.NewGraphConnector(ctx, acct, connector.Sites)
	if err != nil {
		return Only(ctx, errors.Wrap(err, "Failed to connect to Microsoft APIs"))
	}

	sel, err := sharePointBackupCreateSelectors(ctx, site, weburl, gc)
	if err != nil {
		return Only(ctx, errors.Wrap(err, "Retrieving up sharepoint sites by ID and WebURL"))
	}

	var (
		errs *multierror.Error
		bIDs []model.StableID
	)

	for _, scope := range sel.DiscreteScopes(gc.GetSiteIDs()) {
		for _, selSite := range scope.Get(selectors.SharePointSite) {
			opSel := selectors.NewSharePointBackup()
			opSel.Include([]selectors.SharePointScope{scope.DiscreteCopy(selSite)})

			bo, err := r.NewBackup(ctx, opSel.Selector)
			if err != nil {
				errs = multierror.Append(errs, errors.Wrapf(
					err,
					"Failed to initialize SharePoint backup for site %s",
					scope.Get(selectors.SharePointSite),
				))

				continue
			}

			err = bo.Run(ctx)
			if err != nil {
				errs = multierror.Append(errs, errors.Wrapf(
					err,
					"Failed to run SharePoint backup for site %s",
					scope.Get(selectors.SharePointSite),
				))

				continue
			}

			bIDs = append(bIDs, bo.Results.BackupID)
		}
	}

	bups, err := r.Backups(ctx, bIDs)
	if err != nil {
		return Only(ctx, errors.Wrap(err, "Unable to retrieve backup results from storage"))
	}

	backup.PrintAll(ctx, bups)

	if e := errs.ErrorOrNil(); e != nil {
		return Only(ctx, e)
	}

	return nil
}

func validateSharePointBackupCreateFlags(sites, weburls []string) error {
	if len(sites) == 0 && len(weburls) == 0 {
		return errors.New(
			"requires one or more --" +
				utils.SiteFN + " ids, --" +
				utils.WebURLFN + " urls, or the wildcard --" +
				utils.SiteFN + " *",
		)
	}

	return nil
}

func sharePointBackupCreateSelectors(
	ctx context.Context,
	sites, weburls []string,
	gc *connector.GraphConnector,
) (*selectors.SharePointBackup, error) {
	sel := selectors.NewSharePointBackup()

	for _, site := range sites {
		if site == utils.Wildcard {
			sel.Include(sel.Sites(sites))
			return sel, nil
		}
	}

	for _, wURL := range weburls {
		if wURL == utils.Wildcard {
			// due to the wildcard, selectors will drop any url values.
			sel.Include(sel.Sites(weburls))
			return sel, nil
		}
	}

	union, err := gc.UnionSiteIDsAndWebURLs(ctx, sites, weburls)
	if err != nil {
		return nil, err
	}

	sel.Include(sel.Sites(union))

	return sel, nil
}

// ------------------------------------------------------------------------------------------------
// backup list
// ------------------------------------------------------------------------------------------------

// `corso backup list sharepoint [<flag>...]`
func sharePointListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   sharePointServiceCommand,
		Short: "List the history of M365 SharePoint service backups",
		RunE:  listSharePointCmd,
		Args:  cobra.NoArgs,
	}
}

// lists the history of backup operations
func listSharePointCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	s, acct, err := config.GetStorageAndAccount(ctx, true, nil)
	if err != nil {
		return Only(ctx, err)
	}

	r, err := repository.Connect(ctx, acct, s, options.Control())
	if err != nil {
		return Only(ctx, errors.Wrapf(err, "Failed to connect to the %s repository", s.Provider))
	}

	defer utils.CloseRepo(ctx, r)

	if len(backupID) > 0 {
		b, err := r.Backup(ctx, model.StableID(backupID))
		if err != nil {
			if errors.Is(err, kopia.ErrNotFound) {
				return Only(ctx, errors.Errorf("No backup exists with the id %s", backupID))
			}

			return Only(ctx, errors.Wrap(err, "Failed to find backup "+backupID))
		}

		b.Print(ctx)

		return nil
	}

	bs, err := r.BackupsByTag(ctx, store.Service(path.SharePointService))
	if err != nil {
		return Only(ctx, errors.Wrap(err, "Failed to list backups in the repository"))
	}

	backup.PrintAll(ctx, bs)

	return nil
}

// ------------------------------------------------------------------------------------------------
// backup delete
// ------------------------------------------------------------------------------------------------

// `corso backup delete sharepoint [<flag>...]`
func sharePointDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     sharePointServiceCommand,
		Short:   "Delete backed-up M365 SharePoint service data",
		RunE:    deleteSharePointCmd,
		Args:    cobra.NoArgs,
		Example: sharePointServiceCommandDeleteExamples,
	}
}

// deletes a sharePoint service backup.
func deleteSharePointCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if utils.HasNoFlagsAndShownHelp(cmd) {
		return nil
	}

	s, acct, err := config.GetStorageAndAccount(ctx, true, nil)
	if err != nil {
		return Only(ctx, err)
	}

	r, err := repository.Connect(ctx, acct, s, options.Control())
	if err != nil {
		return Only(ctx, errors.Wrapf(err, "Failed to connect to the %s repository", s.Provider))
	}

	defer utils.CloseRepo(ctx, r)

	if err := r.DeleteBackup(ctx, model.StableID(backupID)); err != nil {
		return Only(ctx, errors.Wrapf(err, "Deleting backup %s", backupID))
	}

	Info(ctx, "Deleted SharePoint backup ", backupID)

	return nil
}

// ------------------------------------------------------------------------------------------------
// backup details
// ------------------------------------------------------------------------------------------------

// `corso backup details onedrive [<flag>...]`
func sharePointDetailsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     sharePointServiceCommand,
		Short:   "Shows the details of a M365 SharePoint service backup",
		RunE:    detailsSharePointCmd,
		Args:    cobra.NoArgs,
		Example: sharePointServiceCommandDetailsExamples,
	}
}

// lists the history of backup operations
func detailsSharePointCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if utils.HasNoFlagsAndShownHelp(cmd) {
		return nil
	}

	s, acct, err := config.GetStorageAndAccount(ctx, true, nil)
	if err != nil {
		return Only(ctx, err)
	}

	r, err := repository.Connect(ctx, acct, s, options.Control())
	if err != nil {
		return Only(ctx, errors.Wrapf(err, "Failed to connect to the %s repository", s.Provider))
	}

	defer utils.CloseRepo(ctx, r)

	opts := utils.SharePointOpts{
		LibraryItems: libraryItems,
		LibraryPaths: libraryPaths,
		Sites:        site,
		WebURLs:      weburl,

		Populated: utils.GetPopulatedFlags(cmd),
	}

	ds, err := runDetailsSharePointCmd(ctx, r, backupID, opts)
	if err != nil {
		return Only(ctx, err)
	}

	if len(ds.Entries) == 0 {
		Info(ctx, selectors.ErrorNoMatchingItems)
		return nil
	}

	ds.PrintEntries(ctx)

	return nil
}

// runDetailsSharePointCmd actually performs the lookup in backup details.
func runDetailsSharePointCmd(
	ctx context.Context,
	r repository.BackupGetter,
	backupID string,
	opts utils.SharePointOpts,
) (*details.Details, error) {
	if err := utils.ValidateSharePointRestoreFlags(backupID, opts); err != nil {
		return nil, err
	}

	d, _, err := r.BackupDetails(ctx, backupID)
	if err != nil {
		if errors.Is(err, kopia.ErrNotFound) {
			return nil, errors.Errorf("no backup exists with the id %s", backupID)
		}

		return nil, errors.Wrap(err, "Failed to get backup details in the repository")
	}

	sel := selectors.NewSharePointRestore()
	utils.IncludeSharePointRestoreDataSelectors(sel, opts)
	utils.FilterSharePointRestoreInfoSelectors(sel, opts)

	// if no selector flags were specified, get all data in the service.
	if len(sel.Scopes()) == 0 {
		sel.Include(sel.Sites(selectors.Any()))
	}

	return sel.Reduce(ctx, d), nil
}
