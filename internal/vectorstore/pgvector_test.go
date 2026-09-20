package vectorstore

import (
	"os"
	"strconv"
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConnFromEnv builds a DataConnection from TEST_DATABASE_URL-shaped env
// vars so this package's tests can exercise real Postgres SQL (identifier
// quoting, information_schema introspection) without needing a live org
// credential. Skips if the env isn't configured, matching testutil.SetupTestDB.
func testConnFromEnv(t *testing.T) models.DataConnection {
	t.Helper()
	host := os.Getenv("TEST_PG_HOST")
	if host == "" {
		host = "localhost"
	}
	port, _ := strconv.Atoi(os.Getenv("TEST_PG_PORT"))
	if port == 0 {
		port = 5435
	}
	db := os.Getenv("TEST_PG_DB")
	if db == "" {
		db = "whatomate_test"
	}
	return models.DataConnection{
		Type:     models.DataConnectionTypePostgres,
		Host:     host,
		Port:     port,
		Database: db,
		Username: "whatomate",
		Password: "whatomate",
		SSLMode:  "disable",
	}
}

func TestListPostgresTables_FindsPlainTableAndColumns(t *testing.T) {
	conn := testConnFromEnv(t)
	store, err := newPgVectorStore(conn)
	require.NoError(t, err)
	db, err := store.open()
	if err != nil {
		t.Skipf("no local Postgres available for this test: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("no local Postgres available for this test: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS wadiag_probe_table (id uuid, note text, score int)`)
	require.NoError(t, err)
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS wadiag_probe_table`) })

	tables, err := ListPostgresTables(conn, "")
	require.NoError(t, err)

	var found *TableInfo
	for i := range tables {
		if tables[i].Name == "wadiag_probe_table" {
			found = &tables[i]
			break
		}
	}
	require.NotNil(t, found, "expected wadiag_probe_table to be listed")
	assert.False(t, found.HasVectorColumn, "plain table has no vector column")

	colNames := make(map[string]string)
	for _, c := range found.Columns {
		colNames[c.Name] = c.DataType
	}
	assert.Equal(t, "uuid", colNames["id"])
	assert.Equal(t, "text", colNames["note"])
	assert.Equal(t, "integer", colNames["score"])
}
