package worker

import (
	"testing"

	"github.com/example/controlplane/internal/domain"
)

func TestApplyTransferOptionsPostgres(t *testing.T) {
	validate := false
	env := map[string]string{}
	err := applyTransferOptions(env, "target-postgres", domain.PipelineTransferOptions{
		BatchSizeRows:   50000,
		ValidateRecords: &validate,
		LoadMethod:      "append-only",
	})
	if err != nil {
		t.Fatalf("applyTransferOptions: %v", err)
	}
	if env["TARGET_POSTGRES_BATCH_SIZE_ROWS"] != "50000" ||
		env["TARGET_POSTGRES_VALIDATE_RECORDS"] != "false" ||
		env["TARGET_POSTGRES_LOAD_METHOD"] != "append-only" {
		t.Fatalf("transfer env = %#v", env)
	}
}

func TestApplyTransferOptionsRejectsOtherTarget(t *testing.T) {
	err := applyTransferOptions(map[string]string{}, "target-mysql", domain.PipelineTransferOptions{
		BatchSizeRows: 50000,
	})
	if err == nil {
		t.Fatal("target selain PostgreSQL harus ditolak")
	}
}
