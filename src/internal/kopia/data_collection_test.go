package kopia

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/alcionai/corso/internal/data"
)

// ---------------
// unit tests
// ---------------
type KopiaDataCollectionUnitSuite struct {
	suite.Suite
}

func TestKopiaDataCollectionUnitSuite(t *testing.T) {
	suite.Run(t, new(KopiaDataCollectionUnitSuite))
}

func (suite *KopiaDataCollectionUnitSuite) TestReturnsPath() {
	t := suite.T()

	path := []string{"some", "path", "for", "data"}

	c := kopiaDataCollection{
		streams: []data.Stream{},
		path:    path,
	}

	assert.Equal(t, c.FullPath(), path)
}

func (suite *KopiaDataCollectionUnitSuite) TestReturnsStreams() {
	testData := [][]byte{
		[]byte("abcdefghijklmnopqrstuvwxyz"),
		[]byte("zyxwvutsrqponmlkjihgfedcba"),
	}

	uuids := []string{
		"a-file",
		"another-file",
	}

	table := []struct {
		name    string
		streams []data.Stream
	}{
		{
			name: "SingleStream",
			streams: []data.Stream{
				&kopiaDataStream{
					reader: io.NopCloser(bytes.NewReader(testData[0])),
					uuid:   uuids[0],
				},
			},
		},
		{
			name: "MultipleStreams",
			streams: []data.Stream{
				&kopiaDataStream{
					reader: io.NopCloser(bytes.NewReader(testData[0])),
					uuid:   uuids[0],
				},
				&kopiaDataStream{
					reader: io.NopCloser(bytes.NewReader(testData[1])),
					uuid:   uuids[1],
				},
			},
		},
	}

	for _, test := range table {
		suite.T().Run(test.name, func(t *testing.T) {
			c := kopiaDataCollection{
				streams: test.streams,
				path:    []string{},
			}

			count := 0
			for returnedStream := range c.Items() {
				require.Less(t, count, len(test.streams))

				assert.Equal(t, returnedStream.UUID(), uuids[count])

				buf, err := ioutil.ReadAll(returnedStream.ToReader())
				require.NoError(t, err)
				assert.Equal(t, buf, testData[count])

				count++
			}

			assert.Equal(t, len(test.streams), count)
		})
	}
}
