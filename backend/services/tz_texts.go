package services

import "main/domain"

// Время в текстах бота по поясу новичка (FEATURE_TZ). Тексты, где есть время или день
// («Первые материалы пришлю завтра в 10:00», {start_day}, «Напомнить завтра»), считают его по
// календарю SchedulerService.SiteFor: с флагом — Employee.TZ и окно 8–18, без флага — пояс
// площадки, как раньше (domain.SiteOf).

// siteFor — календарь площадки, если календаря новичка нет (шаблоны без планировщика, тесты).
func (r recipient) siteFor(e *domain.Employee) domain.Site {
	if r.site != nil {
		return *r.site
	}
	return domain.SiteOf(e.Site)
}

// recipient — адресат новичка с его календарём для времени в текстах.
func (s *SchedulerService) recipient(e *domain.Employee) recipient {
	r := recipientOf(e)
	if s != nil && s.features.TZ {
		site := s.SiteFor(e)
		r.site = &site
	}
	return r
}

// siteFor — календарь новичка для текстов бота: SiteFor планировщика (без FEATURE_TZ — пояс
// площадки).
func (b *BotService) siteFor(e *domain.Employee) domain.Site {
	if b.scheduler != nil {
		return b.scheduler.SiteFor(e)
	}
	return domain.SiteOf(e.Site)
}

// recipient — адресат новичка с его календарём для времени в текстах.
func (b *BotService) recipient(e *domain.Employee) recipient {
	return b.scheduler.recipient(e)
}
