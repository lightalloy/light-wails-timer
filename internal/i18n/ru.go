// Package i18n holds the user-visible Russian strings of the application.
package i18n

import "fmt"

// AppName is used for the tray label, tooltip and window title.
const AppName = "light-wails-timer"

// Tray tooltip.
const (
	TooltipIdle = "неактивен"
)

// Tray menu.
const (
	MenuStatusIdle          = "Таймер неактивен"
	MenuStatusRunningFormat = "Таймер активен (%d мин)"
	MenuStatusPausedFormat  = "Таймер на паузе (%d мин)"
	MenuStartFormat         = "Запустить на %d минут"
	MenuPause               = "Пауза"
	MenuResume              = "Продолжить"
	MenuStop                = "Остановить"
	MenuShow                = "Показать окно"
	MenuAutostart           = "Запускать при входе"
	MenuQuit                = "Выход"
)

// Notifications.
const (
	NotificationTitle      = "Таймер завершён"
	NotificationBodyFormat = "Интервал %d минут истёк"
	NotificationMissedBody = "Интервал завершился, пока приложение было закрыто"
)

// Window notices for degraded modes.
const (
	NoticeNotifyFailed    = "Не удалось показать уведомление"
	NoticeAutostartFailed = "Не удалось изменить автозапуск"
	NoticeStateReadFailed = "Не удалось прочитать сохранённое состояние"
	NoticeSaveFailed      = "Не удалось сохранить состояние таймера"
)

// Tooltip formats the tray tooltip for the given remaining time.
func Tooltip(remaining string) string {
	return fmt.Sprintf("%s — %s", AppName, remaining)
}

// TooltipIdleText is the tooltip for the inactive state.
func TooltipIdleText() string {
	return Tooltip(TooltipIdle)
}

// MenuStart formats the menu item that starts an interval.
func MenuStart(minutes int) string {
	return fmt.Sprintf(MenuStartFormat, minutes)
}

// MenuStatusRunning formats the menu status line for a running timer.
func MenuStatusRunning(minutes int) string {
	return fmt.Sprintf(MenuStatusRunningFormat, minutes)
}

// MenuStatusPaused formats the menu status line for a paused timer.
func MenuStatusPaused(minutes int) string {
	return fmt.Sprintf(MenuStatusPausedFormat, minutes)
}
