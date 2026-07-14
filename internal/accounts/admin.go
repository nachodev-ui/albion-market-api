package accounts

import (
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/accountadmin"
	"github.com/nachodev-ui/albion-market-api/internal/adminpanel"
)

const adminPanelMaxGrantDuration = 365 * 24 * time.Hour

func (h *Handler) AdminHandler() *adminpanel.Handler {
	if h == nil || h.service == nil || h.service.db == nil {
		return nil
	}
	accountRepository, err := accountadmin.NewPostgresRepository(h.service.db)
	if err != nil {
		return nil
	}
	accountService, err := accountadmin.NewService(accountRepository, "production", adminPanelMaxGrantDuration)
	if err != nil {
		return nil
	}
	panelRepository, err := adminpanel.NewPostgresRepository(h.service.db)
	if err != nil {
		return nil
	}
	panelService, err := adminpanel.NewService(panelRepository, accountService)
	if err != nil {
		return nil
	}
	return adminpanel.NewHandler(panelService)
}
