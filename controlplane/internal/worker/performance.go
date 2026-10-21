package worker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/example/controlplane/internal/domain"
)

func applyTransferOptions(
	env map[string]string,
	loader string,
	options domain.PipelineTransferOptions,
) error {
	if options.BatchSizeRows == 0 && options.ValidateRecords == nil && options.LoadMethod == "" {
		return nil
	}
	base, _, _ := strings.Cut(loader, "--")
	if base != "target-postgres" {
		return fmt.Errorf("transfer_options hanya didukung target-postgres")
	}
	if options.BatchSizeRows > 0 {
		value := strconv.Itoa(options.BatchSizeRows)
		env["MELTANO_LOAD_BATCH_SIZE_ROWS"] = value
		env["TARGET_POSTGRES_BATCH_SIZE_ROWS"] = value
	}
	if options.ValidateRecords != nil {
		value := strconv.FormatBool(*options.ValidateRecords)
		env["MELTANO_LOAD_VALIDATE_RECORDS"] = value
		env["TARGET_POSTGRES_VALIDATE_RECORDS"] = value
	}
	if options.LoadMethod != "" {
		env["MELTANO_LOAD_LOAD_METHOD"] = options.LoadMethod
		env["TARGET_POSTGRES_LOAD_METHOD"] = options.LoadMethod
	}
	return nil
}
