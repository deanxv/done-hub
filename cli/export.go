package cli

import (
	"go-template/common/logger"
)

// ExportPrices is not supported in the minimal edition; keep a stub to satisfy CLI flag.
func ExportPrices() {
	logger.SysLog("ExportPrices is disabled in the minimal edition")
}
