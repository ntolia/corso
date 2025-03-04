package kopia

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/alcionai/corso/src/internal/model"
	"github.com/alcionai/corso/src/internal/tester"
	"github.com/alcionai/corso/src/pkg/backup"
)

type fooModel struct {
	model.BaseModel
	Bar string
}

//revive:disable:context-as-argument
func getModelStore(t *testing.T, ctx context.Context) *ModelStore {
	//revive:enable:context-as-argument
	c, err := openKopiaRepo(t, ctx)
	require.NoError(t, err)

	return &ModelStore{c: c, modelVersion: globalModelVersion}
}

// ---------------
// unit tests
// ---------------
type ModelStoreUnitSuite struct {
	suite.Suite
}

func TestModelStoreUnitSuite(t *testing.T) {
	suite.Run(t, new(ModelStoreUnitSuite))
}

func (suite *ModelStoreUnitSuite) TestCloseWithoutInitDoesNotPanic() {
	assert.NotPanics(suite.T(), func() {
		ctx, flush := tester.NewContext()
		defer flush()

		m := &ModelStore{}
		m.Close(ctx)
	})
}

// ---------------
// integration tests that use kopia
// ---------------
type ModelStoreIntegrationSuite struct {
	suite.Suite
	ctx   context.Context
	m     *ModelStore
	flush func()
}

func TestModelStoreIntegrationSuite(t *testing.T) {
	if err := tester.RunOnAny(
		tester.CorsoCITests,
		tester.CorsoModelStoreTests,
	); err != nil {
		t.Skip(err)
	}

	suite.Run(t, new(ModelStoreIntegrationSuite))
}

func (suite *ModelStoreIntegrationSuite) SetupSuite() {
	_, err := tester.GetRequiredEnvVars(tester.AWSStorageCredEnvs...)
	require.NoError(suite.T(), err)
}

func (suite *ModelStoreIntegrationSuite) SetupTest() {
	suite.ctx, suite.flush = tester.NewContext()
	suite.m = getModelStore(suite.T(), suite.ctx)
}

func (suite *ModelStoreIntegrationSuite) TearDownTest() {
	defer suite.flush()
	assert.NoError(suite.T(), suite.m.Close(suite.ctx))
}

func (suite *ModelStoreIntegrationSuite) TestBadTagsErrors() {
	table := []struct {
		name string
		tags map[string]string
	}{
		{
			name: "StableIDTag",
			tags: map[string]string{
				stableIDKey: "foo",
			},
		},
		{
			name: "manifestTypeTag",
			tags: map[string]string{
				manifest.TypeLabelKey: "foo",
			},
		},
		{
			name: "storeVersion",
			tags: map[string]string{
				manifest.TypeLabelKey: "foo",
			},
		},
	}

	for _, test := range table {
		suite.T().Run(test.name, func(t *testing.T) {
			foo := &fooModel{Bar: uuid.NewString()}
			foo.Tags = test.tags

			assert.ErrorIs(
				t,
				suite.m.Put(suite.ctx, model.BackupOpSchema, foo),
				errBadTagKey,
			)

			// Add model for update/get ID checks.
			foo.Tags = map[string]string{}
			require.NoError(
				t,
				suite.m.Put(suite.ctx, model.BackupOpSchema, foo),
			)

			foo.Tags = test.tags
			assert.ErrorIs(
				t,
				suite.m.Update(suite.ctx, model.BackupOpSchema, foo),
				errBadTagKey,
			)

			_, err := suite.m.GetIDsForType(
				suite.ctx,
				model.BackupOpSchema,
				test.tags,
			)
			assert.ErrorIs(t, err, errBadTagKey)
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestNoIDsErrors() {
	t := suite.T()
	theModelType := model.BackupOpSchema

	noStableID := &fooModel{Bar: uuid.NewString()}
	noStableID.ID = ""
	noStableID.ModelStoreID = manifest.ID(uuid.NewString())

	noModelStoreID := &fooModel{Bar: uuid.NewString()}
	noModelStoreID.ID = model.StableID(uuid.NewString())
	noModelStoreID.ModelStoreID = ""

	assert.Error(t, suite.m.Update(suite.ctx, theModelType, noStableID))
	assert.Error(t, suite.m.Update(suite.ctx, theModelType, noModelStoreID))

	assert.Error(t, suite.m.Get(suite.ctx, theModelType, "", nil))
	assert.Error(t, suite.m.GetWithModelStoreID(suite.ctx, theModelType, "", nil))

	assert.Error(t, suite.m.Delete(suite.ctx, theModelType, ""))
	assert.Error(t, suite.m.DeleteWithModelStoreID(suite.ctx, ""))
}

func (suite *ModelStoreIntegrationSuite) TestBadModelTypeErrors() {
	t := suite.T()

	foo := &fooModel{Bar: uuid.NewString()}

	assert.ErrorIs(
		t,
		suite.m.Put(suite.ctx, model.UnknownSchema, foo),
		errUnrecognizedSchema,
	)

	require.NoError(t, suite.m.Put(suite.ctx, model.BackupOpSchema, foo))

	_, err := suite.m.GetIDsForType(suite.ctx, model.UnknownSchema, nil)
	assert.ErrorIs(t, err, errUnrecognizedSchema)
}

func (suite *ModelStoreIntegrationSuite) TestBadTypeErrors() {
	t := suite.T()

	foo := &fooModel{Bar: uuid.NewString()}

	require.NoError(t, suite.m.Put(suite.ctx, model.BackupOpSchema, foo))

	returned := &fooModel{}
	assert.ErrorIs(
		t,
		suite.m.Get(suite.ctx, model.RestoreOpSchema, foo.ID, returned),
		errModelTypeMismatch,
	)

	assert.ErrorIs(
		t,
		suite.m.GetWithModelStoreID(suite.ctx, model.RestoreOpSchema, foo.ModelStoreID, returned),
		errModelTypeMismatch,
	)

	assert.ErrorIs(
		t,
		suite.m.Delete(suite.ctx, model.RestoreOpSchema, foo.ID),
		errModelTypeMismatch,
	)
}

func (suite *ModelStoreIntegrationSuite) TestPutGetBadVersion() {
	t := suite.T()
	schema := model.BackupOpSchema
	foo := &fooModel{Bar: uuid.NewString()}
	// Avoid some silly test errors from comparing nil to empty map.
	foo.Tags = map[string]string{}

	err := suite.m.Put(suite.ctx, schema, foo)
	require.NoError(t, err)

	suite.m.modelVersion = 42

	returned := &fooModel{}
	err = suite.m.Get(suite.ctx, schema, foo.ID, returned)
	assert.Error(t, err)
}

func (suite *ModelStoreIntegrationSuite) TestPutGet() {
	table := []struct {
		s      model.Schema
		check  require.ErrorAssertionFunc
		hasErr bool
	}{
		{
			s:      model.UnknownSchema,
			check:  require.Error,
			hasErr: true,
		},
		{
			s:      model.BackupOpSchema,
			check:  require.NoError,
			hasErr: false,
		},
		{
			s:      model.RestoreOpSchema,
			check:  require.NoError,
			hasErr: false,
		},
		{
			s:      model.BackupSchema,
			check:  require.NoError,
			hasErr: false,
		},
	}

	for _, test := range table {
		suite.T().Run(test.s.String(), func(t *testing.T) {
			foo := &fooModel{Bar: uuid.NewString()}
			// Avoid some silly test errors from comparing nil to empty map.
			foo.Tags = map[string]string{}

			err := suite.m.Put(suite.ctx, test.s, foo)
			test.check(t, err)

			if test.hasErr {
				return
			}

			require.NotEmpty(t, foo.ModelStoreID)
			require.NotEmpty(t, foo.ID)
			require.Equal(t, globalModelVersion, foo.Version)

			returned := &fooModel{}
			err = suite.m.Get(suite.ctx, test.s, foo.ID, returned)
			require.NoError(t, err)
			assert.Equal(t, foo, returned)

			err = suite.m.GetWithModelStoreID(suite.ctx, test.s, foo.ModelStoreID, returned)
			require.NoError(t, err)
			assert.Equal(t, foo, returned)
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestPutGet_PreSetID() {
	mdl := model.BackupOpSchema
	table := []struct {
		name   string
		baseID string
		expect assert.ComparisonAssertionFunc
	}{
		{
			name:   "genreate new id",
			baseID: "",
			expect: assert.NotEqual,
		},
		{
			name:   "use provided id",
			baseID: uuid.NewString(),
			expect: assert.Equal,
		},
	}

	for _, test := range table {
		suite.T().Run(test.name, func(t *testing.T) {
			foo := &fooModel{
				BaseModel: model.BaseModel{ID: model.StableID(test.baseID)},
				Bar:       uuid.NewString(),
			}

			// Avoid some silly test errors from comparing nil to empty map.
			foo.Tags = map[string]string{}

			err := suite.m.Put(suite.ctx, mdl, foo)
			require.NoError(t, err)

			test.expect(t, model.StableID(test.baseID), foo.ID)
			require.NotEmpty(t, foo.ModelStoreID)
			require.NotEmpty(t, foo.ID)

			returned := &fooModel{}
			err = suite.m.Get(suite.ctx, mdl, foo.ID, returned)
			require.NoError(t, err)
			assert.Equal(t, foo, returned)

			err = suite.m.GetWithModelStoreID(suite.ctx, mdl, foo.ModelStoreID, returned)
			require.NoError(t, err)
			assert.Equal(t, foo, returned)
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestPutGet_WithTags() {
	t := suite.T()
	theModelType := model.BackupOpSchema

	foo := &fooModel{Bar: uuid.NewString()}
	foo.Tags = map[string]string{
		"bar": "baz",
	}

	require.NoError(t, suite.m.Put(suite.ctx, theModelType, foo))

	require.NotEmpty(t, foo.ModelStoreID)
	require.NotEmpty(t, foo.ID)

	returned := &fooModel{}
	err := suite.m.Get(suite.ctx, theModelType, foo.ID, returned)
	require.NoError(t, err)
	assert.Equal(t, foo, returned)

	err = suite.m.GetWithModelStoreID(suite.ctx, theModelType, foo.ModelStoreID, returned)
	require.NoError(t, err)
	assert.Equal(t, foo, returned)
}

func (suite *ModelStoreIntegrationSuite) TestGet_NotFoundErrors() {
	t := suite.T()

	assert.ErrorIs(t, suite.m.Get(suite.ctx, model.BackupOpSchema, "baz", nil), ErrNotFound)
	assert.ErrorIs(
		t, suite.m.GetWithModelStoreID(suite.ctx, model.BackupOpSchema, "baz", nil), ErrNotFound)
}

func (suite *ModelStoreIntegrationSuite) TestPutGetOfTypeBadVersion() {
	t := suite.T()
	schema := model.BackupOpSchema

	foo := &fooModel{Bar: uuid.NewString()}

	err := suite.m.Put(suite.ctx, schema, foo)
	require.NoError(t, err)

	suite.m.modelVersion = 42

	ids, err := suite.m.GetIDsForType(suite.ctx, schema, nil)
	assert.Error(t, err)
	assert.Empty(t, ids)
}

func (suite *ModelStoreIntegrationSuite) TestPutGetOfType() {
	table := []struct {
		s      model.Schema
		check  require.ErrorAssertionFunc
		hasErr bool
	}{
		{
			s:      model.UnknownSchema,
			check:  require.Error,
			hasErr: true,
		},
		{
			s:      model.BackupOpSchema,
			check:  require.NoError,
			hasErr: false,
		},
		{
			s:      model.RestoreOpSchema,
			check:  require.NoError,
			hasErr: false,
		},
		{
			s:      model.BackupSchema,
			check:  require.NoError,
			hasErr: false,
		},
	}

	for _, test := range table {
		suite.T().Run(test.s.String(), func(t *testing.T) {
			foo := &fooModel{Bar: uuid.NewString()}

			err := suite.m.Put(suite.ctx, test.s, foo)
			test.check(t, err)

			if test.hasErr {
				return
			}

			ids, err := suite.m.GetIDsForType(suite.ctx, test.s, nil)
			require.NoError(t, err)

			assert.Len(t, ids, 1)
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestGetOfTypeWithTags() {
	tagKey1 := "foo"
	tagKey2 := "bar"
	tagValue1 := "hola"
	tagValue2 := "mundo"

	inputs := []struct {
		schema    model.Schema
		dataModel *fooModel
	}{
		{
			schema: model.BackupOpSchema,
			dataModel: &fooModel{
				BaseModel: model.BaseModel{
					Tags: map[string]string{
						tagKey1: tagValue1,
					},
				},
			},
		},
		{
			schema: model.BackupOpSchema,
			dataModel: &fooModel{
				BaseModel: model.BaseModel{
					Tags: map[string]string{
						tagKey1: tagValue1,
						tagKey2: tagValue2,
					},
				},
			},
		},
		{
			schema: model.BackupOpSchema,
			dataModel: &fooModel{
				BaseModel: model.BaseModel{
					Tags: map[string]string{
						tagKey1: tagValue2,
					},
				},
			},
		},
		{
			schema: model.RestoreOpSchema,
			dataModel: &fooModel{
				BaseModel: model.BaseModel{
					Tags: map[string]string{
						tagKey1: tagValue1,
					},
				},
			},
		},
		{
			schema: model.RestoreOpSchema,
			dataModel: &fooModel{
				BaseModel: model.BaseModel{
					Tags: map[string]string{},
				},
			},
		},
	}

	table := []struct {
		name           string
		s              model.Schema
		tags           map[string]string
		expectedModels []*fooModel
	}{
		{
			name: "UnpopulatedType",
			s:    model.BackupSchema,
			tags: map[string]string{
				tagKey1: tagValue1,
			},
			expectedModels: []*fooModel{},
		},
		{
			name: "RestrictByModelType",
			s:    model.RestoreOpSchema,
			tags: map[string]string{
				tagKey1: tagValue1,
			},
			expectedModels: []*fooModel{inputs[3].dataModel},
		},
		{
			name: "RestrictByModelType2",
			s:    model.BackupOpSchema,
			tags: map[string]string{
				tagKey1: tagValue1,
			},
			expectedModels: []*fooModel{inputs[0].dataModel, inputs[1].dataModel},
		},
		{
			name: "RestrictByTags",
			s:    model.BackupOpSchema,
			tags: map[string]string{
				tagKey1: tagValue1,
				tagKey2: tagValue2,
			},
			expectedModels: []*fooModel{inputs[1].dataModel},
		},
		{
			name: "RestrictByTags2",
			s:    model.BackupOpSchema,
			tags: map[string]string{
				tagKey1: tagValue2,
			},
			expectedModels: []*fooModel{inputs[2].dataModel},
		},
	}

	// Setup the store by adding all the inputs.
	for _, in := range inputs {
		require.NoError(suite.T(), suite.m.Put(suite.ctx, in.schema, in.dataModel))
	}

	// Check we can properly execute our tests.
	for _, test := range table {
		suite.T().Run(test.name, func(t *testing.T) {
			expected := make([]*model.BaseModel, 0, len(test.expectedModels))
			for _, e := range test.expectedModels {
				expected = append(expected, &e.BaseModel)
			}

			ids, err := suite.m.GetIDsForType(suite.ctx, test.s, test.tags)
			require.NoError(t, err)

			assert.ElementsMatch(t, expected, ids)
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestPutUpdate() {
	table := []struct {
		name    string
		mutator func(m *fooModel)
	}{
		{
			name: "NoTags",
			mutator: func(m *fooModel) {
				m.Bar = "baz"
				m.Version = 42
			},
		},
		{
			name: "WithTags",
			mutator: func(m *fooModel) {
				m.Bar = "baz"
				m.Version = 42
				m.Tags = map[string]string{
					"a": "42",
				}
			},
		},
	}

	for _, test := range table {
		suite.T().Run(test.name, func(t *testing.T) {
			ctx, flush := tester.NewContext()
			defer flush()

			theModelType := model.BackupOpSchema

			m := getModelStore(t, ctx)
			defer func() {
				assert.NoError(t, m.c.Close(ctx))
			}()

			foo := &fooModel{Bar: uuid.NewString()}
			// Avoid some silly test errors from comparing nil to empty map.
			foo.Tags = map[string]string{}

			require.NoError(t, m.Put(ctx, theModelType, foo))

			oldModelID := foo.ModelStoreID
			oldStableID := foo.ID
			oldVersion := foo.Version

			test.mutator(foo)

			require.NoError(t, m.Update(ctx, theModelType, foo))
			assert.Equal(t, oldStableID, foo.ID)
			// The version in the model store has not changed so we get the old
			// version back.
			assert.Equal(t, oldVersion, foo.Version)

			returned := &fooModel{}
			require.NoError(
				t, m.GetWithModelStoreID(ctx, theModelType, foo.ModelStoreID, returned))
			assert.Equal(t, foo, returned)

			ids, err := m.GetIDsForType(ctx, theModelType, nil)
			require.NoError(t, err)
			require.Len(t, ids, 1)
			assert.Equal(t, globalModelVersion, ids[0].Version)

			if oldModelID == foo.ModelStoreID {
				// Unlikely, but we don't control ModelStoreID generation and can't
				// guarantee this won't happen.
				return
			}

			err = m.GetWithModelStoreID(ctx, theModelType, oldModelID, nil)
			assert.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestPutUpdate_FailsNotMatchingPrev() {
	startModelType := model.BackupOpSchema

	table := []struct {
		name    string
		s       model.Schema
		mutator func(m *fooModel)
	}{
		{
			name: "DifferentModelStoreID",
			s:    startModelType,
			mutator: func(m *fooModel) {
				m.ModelStoreID = manifest.ID("bar")
			},
		},
		{
			name: "DifferentModelType",
			s:    model.RestoreOpSchema,
			mutator: func(m *fooModel) {
			},
		},
	}

	for _, test := range table {
		suite.T().Run(test.name, func(t *testing.T) {
			ctx, flush := tester.NewContext()
			defer flush()

			m := getModelStore(t, ctx)
			defer func() {
				assert.NoError(t, m.c.Close(ctx))
			}()

			foo := &fooModel{Bar: uuid.NewString()}

			require.NoError(t, m.Put(ctx, startModelType, foo))

			test.mutator(foo)

			assert.Error(t, m.Update(ctx, test.s, foo))
		})
	}
}

func (suite *ModelStoreIntegrationSuite) TestPutDelete() {
	t := suite.T()
	theModelType := model.BackupOpSchema

	foo := &fooModel{Bar: uuid.NewString()}

	require.NoError(t, suite.m.Put(suite.ctx, theModelType, foo))

	require.NoError(t, suite.m.Delete(suite.ctx, theModelType, foo.ID))

	returned := &fooModel{}
	err := suite.m.GetWithModelStoreID(suite.ctx, theModelType, foo.ModelStoreID, returned)
	assert.ErrorIs(t, err, ErrNotFound)
}

func (suite *ModelStoreIntegrationSuite) TestPutDelete_BadIDsNoop() {
	t := suite.T()

	assert.NoError(t, suite.m.Delete(suite.ctx, model.BackupOpSchema, "foo"))
	assert.NoError(t, suite.m.DeleteWithModelStoreID(suite.ctx, "foo"))
}

// ---------------
// regression tests that use kopia
// ---------------
type ModelStoreRegressionSuite struct {
	suite.Suite
}

func TestModelStoreRegressionSuite(t *testing.T) {
	if err := tester.RunOnAny(
		tester.CorsoCITests,
		tester.CorsoModelStoreTests,
	); err != nil {
		t.Skip(err)
	}

	suite.Run(t, new(ModelStoreRegressionSuite))
}

func (suite *ModelStoreRegressionSuite) SetupSuite() {
	_, err := tester.GetRequiredEnvVars(tester.AWSStorageCredEnvs...)
	require.NoError(suite.T(), err)
}

// TODO(ashmrtn): Make a mock of whatever controls the handle to kopia so we can
// ask it to fail on arbitrary function, thus allowing us to directly test
// Update.
// Tests that if we get an error or crash while in the middle of an Update no
// results will be visible to higher layers.
func (suite *ModelStoreRegressionSuite) TestFailDuringWriteSessionHasNoVisibleEffect() {
	ctx, flush := tester.NewContext()
	defer flush()

	t := suite.T()

	m := getModelStore(t, ctx)
	defer func() {
		assert.NoError(t, m.c.Close(ctx))
	}()

	foo := &fooModel{Bar: uuid.NewString()}
	foo.ID = model.StableID(uuid.NewString())
	foo.ModelStoreID = manifest.ID(uuid.NewString())
	// Avoid some silly test errors from comparing nil to empty map.
	foo.Tags = map[string]string{}

	theModelType := model.BackupOpSchema

	require.NoError(t, m.Put(ctx, theModelType, foo))

	newID := manifest.ID("")
	err := repo.WriteSession(
		ctx,
		m.c,
		repo.WriteSessionOptions{Purpose: "WriteSessionFailureTest"},
		func(innerCtx context.Context, w repo.RepositoryWriter) (innerErr error) {
			base := foo.Base()
			oldID := base.ModelStoreID

			defer func() {
				if innerErr != nil {
					// Restore the old ID if we failed.
					base.ModelStoreID = oldID
				}
			}()

			innerErr = putInner(innerCtx, w, theModelType, foo, false)
			require.NoError(t, innerErr)

			newID = foo.ModelStoreID

			return assert.AnError
		},
	)

	assert.ErrorIs(t, err, assert.AnError)

	err = m.GetWithModelStoreID(ctx, theModelType, newID, nil)
	assert.ErrorIs(t, err, ErrNotFound)

	returned := &fooModel{}
	require.NoError(
		t, m.GetWithModelStoreID(ctx, theModelType, foo.ModelStoreID, returned))
	assert.Equal(t, foo, returned)
}

//revive:disable:context-as-argument
func openConnAndModelStore(
	t *testing.T,
	ctx context.Context,
) (*conn, *ModelStore) {
	//revive:enable:context-as-argument
	st := tester.NewPrefixedS3Storage(t)
	c := NewConn(st)

	require.NoError(t, c.Initialize(ctx))

	defer func() {
		require.NoError(t, c.Close(ctx))
	}()

	ms, err := NewModelStore(c)
	require.NoError(t, err)

	return c, ms
}

//revive:disable:context-as-argument
func reconnectToModelStore(
	t *testing.T,
	ctx context.Context,
	c *conn,
) *ModelStore {
	//revive:enable:context-as-argument
	require.NoError(t, c.Connect(ctx))

	defer func() {
		assert.NoError(t, c.Close(ctx))
	}()

	ms, err := NewModelStore(c)
	require.NoError(t, err)

	return ms
}

// Ensures there's no shared configuration state between different instances of
// the ModelStore (and consequently the underlying kopia instances).
func (suite *ModelStoreRegressionSuite) TestMultipleConfigs() {
	ctx, flush := tester.NewContext()
	defer flush()

	t := suite.T()
	backupModel := backup.Backup{
		SnapshotID: "snapshotID",
	}

	conn1, ms1 := openConnAndModelStore(t, ctx)

	require.NoError(t, ms1.Put(ctx, model.BackupSchema, &backupModel))
	require.NoError(t, ms1.Close(ctx))

	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(2)

	var ms2 *ModelStore

	// These need to be opened in parallel because it's a race writing the kopia
	// config file.
	go func() {
		defer ready.Done()

		<-start

		_, ms2 = openConnAndModelStore(t, ctx)
	}()

	go func() {
		defer ready.Done()

		<-start

		ms1 = reconnectToModelStore(t, ctx, conn1)
	}()

	close(start)
	ready.Wait()

	defer func() {
		assert.NoError(t, ms2.Close(ctx))
	}()

	defer func() {
		assert.NoError(t, ms1.Close(ctx))
	}()

	// New instance should not have model we added.
	gotBackup := backup.Backup{}
	err := ms2.GetWithModelStoreID(
		ctx,
		model.BackupSchema,
		backupModel.ModelStoreID,
		&gotBackup,
	)
	assert.Error(t, err)

	// Old instance should still be able to access added model.
	gotBackup = backup.Backup{}
	err = ms1.GetWithModelStoreID(
		ctx,
		model.BackupSchema,
		backupModel.ModelStoreID,
		&gotBackup,
	)
	assert.NoError(t, err)
}
