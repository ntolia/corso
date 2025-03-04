package exchange

import (
	stdpath "path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/alcionai/corso/src/internal/connector/graph"
	"github.com/alcionai/corso/src/internal/tester"
)

const (
	// Need to use a hard-coded ID because GetAllFolderNamesForUser only gets
	// top-level folders right now.
	//nolint:lll
	testFolderID = "AAMkAGZmNjNlYjI3LWJlZWYtNGI4Mi04YjMyLTIxYThkNGQ4NmY1MwAuAAAAAADCNgjhM9QmQYWNcI7hCpPrAQDSEBNbUIB9RL6ePDeF3FIYAABl7AqpAAA="

	//nolint:lll
	topFolderID = "AAMkAGZmNjNlYjI3LWJlZWYtNGI4Mi04YjMyLTIxYThkNGQ4NmY1MwAuAAAAAADCNgjhM9QmQYWNcI7hCpPrAQDSEBNbUIB9RL6ePDeF3FIYAAAAAAEIAAA="
	// Full folder path for the folder above.
	expectedFolderPath = "toplevel/subFolder/subsubfolder"
)

type MailFolderCacheIntegrationSuite struct {
	suite.Suite
	gs graph.Servicer
}

func (suite *MailFolderCacheIntegrationSuite) SetupSuite() {
	t := suite.T()

	_, err := tester.GetRequiredEnvVars(tester.M365AcctCredEnvs...)
	require.NoError(t, err)

	a := tester.NewM365Account(t)
	require.NoError(t, err)

	m365, err := a.M365Config()
	require.NoError(t, err)

	service, err := createService(m365)
	require.NoError(t, err)

	suite.gs = service
}

func TestMailFolderCacheIntegrationSuite(t *testing.T) {
	if err := tester.RunOnAny(
		tester.CorsoCITests,
		tester.CorsoGraphConnectorTests,
		tester.CorsoGraphConnectorExchangeTests,
	); err != nil {
		t.Skip(err)
	}

	suite.Run(t, new(MailFolderCacheIntegrationSuite))
}

func (suite *MailFolderCacheIntegrationSuite) TestDeltaFetch() {
	suite.T().Skipf("Test depends on hardcoded folder names. Skipping till that is fixed")

	ctx, flush := tester.NewContext()
	defer flush()

	tests := []struct {
		name string
		root string
		path []string
	}{
		{
			name: "Default Root",
			root: rootFolderAlias,
		},
		{
			name: "Node Root",
			root: topFolderID,
		},
		{
			name: "Node Root Non-empty Path",
			root: topFolderID,
			path: []string{"some", "leading", "path"},
		},
	}
	userID := tester.M365UserID(suite.T())

	for _, test := range tests {
		suite.T().Run(test.name, func(t *testing.T) {
			mfc := mailFolderCache{
				userID: userID,
				gs:     suite.gs,
			}

			require.NoError(t, mfc.Populate(ctx, test.root, test.path...))

			p, err := mfc.IDToPath(ctx, testFolderID)
			require.NoError(t, err)
			t.Logf("Path: %s\n", p.String())

			expectedPath := stdpath.Join(append(test.path, expectedFolderPath)...)
			assert.Equal(t, expectedPath, p.String())
			identifier, ok := mfc.PathInCache(p.String())
			assert.True(t, ok)
			assert.NotEmpty(t, identifier)
		})
	}
}
