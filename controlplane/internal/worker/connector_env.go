package worker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/controlplane/internal/domain"
)

func isFileOrAPIDriver(driver domain.Driver) bool {
	switch driver {
	case domain.DriverREST, domain.DriverCSV, domain.DriverSheets, domain.DriverS3, domain.DriverDuckDB:
		return true
	default:
		return false
	}
}

func buildFileOrAPIEnv(
	kind domain.ConnectionKind,
	conn domain.Connection,
	secret string,
) (map[string]string, error) {
	switch conn.Driver {
	case domain.DriverREST:
		if kind != domain.KindSource {
			return nil, fmt.Errorf("REST API hanya mendukung source")
		}
		return buildRESTEnv(conn, secret)
	case domain.DriverCSV:
		if kind != domain.KindSource {
			return nil, fmt.Errorf("CSV hanya mendukung source")
		}
		return buildCSVEnv(conn)
	case domain.DriverSheets:
		if kind != domain.KindSource {
			return nil, fmt.Errorf("Google Sheets hanya mendukung source")
		}
		return buildGoogleSheetsEnv(conn, secret)
	case domain.DriverS3:
		if kind != domain.KindDestination {
			return nil, fmt.Errorf("Amazon S3 hanya mendukung destination")
		}
		return buildS3Env(conn, secret)
	case domain.DriverDuckDB:
		if kind != domain.KindDestination {
			return nil, fmt.Errorf("DuckDB hanya mendukung destination")
		}
		return buildDuckDBEnv(conn)
	default:
		return nil, fmt.Errorf("driver %s bukan connector file/API", conn.Driver)
	}
}

func buildRESTEnv(conn domain.Connection, secret string) (map[string]string, error) {
	apiURL := connectionString(conn, "api_url")
	if apiURL == "" {
		return nil, fmt.Errorf("%w: api_url REST wajib diisi", domain.ErrInvalid)
	}
	authMethod := strings.ToLower(connectionString(conn, "auth_method"))
	if authMethod == "" {
		authMethod = "no_auth"
	}
	env := map[string]string{
		"TAP_REST_API_MSDK_API_URL":     apiURL,
		"TAP_REST_API_MSDK_AUTH_METHOD": authMethod,
	}
	streamName := connectionString(conn, "stream_name")
	if streamName == "" {
		return nil, fmt.Errorf("%w: stream_name REST wajib diisi", domain.ErrInvalid)
	}
	stream := map[string]any{
		"name":         streamName,
		"primary_keys": endpointStringList(conn.Endpoint["primary_keys"]),
	}
	if path := connectionString(conn, "path"); path != "" {
		env["TAP_REST_API_MSDK_PATH"] = path
		stream["path"] = path
	}
	if recordsPath := connectionString(conn, "records_path"); recordsPath != "" {
		stream["records_path"] = recordsPath
	}
	encodedStreams, err := json.Marshal([]map[string]any{stream})
	if err != nil {
		return nil, err
	}
	env["TAP_REST_API_MSDK_STREAMS"] = string(encodedStreams)
	switch authMethod {
	case "no_auth":
	case "basic":
		env["TAP_REST_API_MSDK_USERNAME"] = conn.Username
		env["TAP_REST_API_MSDK_PASSWORD"] = secret
	case "bearer_token":
		env["TAP_REST_API_MSDK_BEARER_TOKEN"] = secret
	case "api_key":
		header := connectionString(conn, "api_key_header")
		if header == "" {
			header = "Authorization"
		}
		encoded, err := json.Marshal(map[string]string{header: secret})
		if err != nil {
			return nil, err
		}
		env["TAP_REST_API_MSDK_API_KEYS"] = string(encoded)
	default:
		return nil, fmt.Errorf("%w: auth_method REST %q tidak didukung", domain.ErrInvalid, authMethod)
	}
	return env, nil
}

func buildCSVEnv(conn domain.Connection) (map[string]string, error) {
	path := connectionString(conn, "path")
	entity := connectionString(conn, "entity")
	if path == "" || entity == "" {
		return nil, fmt.Errorf("%w: path dan entity CSV wajib diisi", domain.ErrInvalid)
	}
	file := map[string]any{
		"entity": entity,
		"path":   path,
		"keys":   endpointStringList(conn.Endpoint["keys"]),
	}
	if encoding := connectionString(conn, "encoding"); encoding != "" {
		file["encoding"] = encoding
	}
	encoded, err := json.Marshal([]map[string]any{file})
	if err != nil {
		return nil, err
	}
	return map[string]string{"TAP_CSV_FILES": string(encoded)}, nil
}

func endpointStringList(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		out := []string{}
		for _, item := range strings.Split(values, ",") {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	default:
		return []string{}
	}
}

func buildGoogleSheetsEnv(conn domain.Connection, secret string) (map[string]string, error) {
	sheetID := connectionString(conn, "sheet_id")
	if sheetID == "" {
		return nil, fmt.Errorf("%w: sheet_id Google Sheets wajib diisi", domain.ErrInvalid)
	}
	credentials := map[string]string{}
	if err := json.Unmarshal([]byte(secret), &credentials); err != nil {
		return nil, fmt.Errorf("%w: secret Google Sheets harus JSON OAuth", domain.ErrInvalid)
	}
	clientID := credentials["client_id"]
	clientSecret := credentials["client_secret"]
	refreshToken := credentials["refresh_token"]
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		return nil, fmt.Errorf("%w: OAuth Google Sheets memerlukan client_id, client_secret, refresh_token", domain.ErrInvalid)
	}
	env := map[string]string{
		"TAP_GOOGLE_SHEETS_SHEET_ID":                        sheetID,
		"TAP_GOOGLE_SHEETS_OAUTH_CREDENTIALS_CLIENT_ID":     clientID,
		"TAP_GOOGLE_SHEETS_OAUTH_CREDENTIALS_CLIENT_SECRET": clientSecret,
		"TAP_GOOGLE_SHEETS_OAUTH_CREDENTIALS_REFRESH_TOKEN": refreshToken,
	}
	if child := connectionString(conn, "child_sheet_name"); child != "" {
		env["TAP_GOOGLE_SHEETS_CHILD_SHEET_NAME"] = child
	}
	if cellRange := connectionString(conn, "range"); cellRange != "" {
		env["TAP_GOOGLE_SHEETS_RANGE"] = cellRange
	}
	return env, nil
}

func buildS3Env(conn domain.Connection, secret string) (map[string]string, error) {
	bucket := connectionString(conn, "bucket")
	region := connectionString(conn, "region")
	format := connectionString(conn, "format")
	if bucket == "" || region == "" || format == "" {
		return nil, fmt.Errorf("%w: bucket, region, dan format S3 wajib diisi", domain.ErrInvalid)
	}
	env := map[string]string{
		"TARGET_S3_CLOUD_PROVIDER_CLOUD_PROVIDER_TYPE": "aws",
		"TARGET_S3_CLOUD_PROVIDER_AWS_AWS_BUCKET":      bucket,
		"TARGET_S3_CLOUD_PROVIDER_AWS_AWS_REGION":      region,
		"TARGET_S3_FORMAT_FORMAT_TYPE":                 format,
	}
	if conn.Username != "" {
		env["TARGET_S3_CLOUD_PROVIDER_AWS_AWS_ACCESS_KEY_ID"] = conn.Username
	}
	if secret != "" {
		env["TARGET_S3_CLOUD_PROVIDER_AWS_AWS_SECRET_ACCESS_KEY"] = secret
	}
	if prefix := connectionString(conn, "prefix"); prefix != "" {
		env["TARGET_S3_PREFIX"] = prefix
	}
	if endpoint := connectionString(conn, "endpoint_override"); endpoint != "" {
		env["TARGET_S3_CLOUD_PROVIDER_AWS_AWS_ENDPOINT_OVERRIDE"] = endpoint
	}
	return env, nil
}

func buildDuckDBEnv(conn domain.Connection) (map[string]string, error) {
	path := connectionString(conn, "path")
	if path == "" {
		return nil, fmt.Errorf("%w: filepath DuckDB wajib diisi", domain.ErrInvalid)
	}
	schema := connectionString(conn, "schema")
	if schema == "" {
		schema = "main"
	}
	return map[string]string{
		"TARGET_DUCKDB_FILEPATH":              path,
		"TARGET_DUCKDB_DEFAULT_TARGET_SCHEMA": schema,
	}, nil
}
