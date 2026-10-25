package tui

import (
	"shepherd/internal/store"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// numSettings is the count of editable rows in the settings screen, in the
// order settingsView renders them: view, density, autosave, categories,
// statuses, footer, mouse.
const numSettings = 7

func (m *model) saveSettings() { _ = saveConfig(store.ConfigPath(), m.currentConfig()) }

// updateSettings handles the settings screen: navigate rows, cycle the enum
// rows (view/density) in place, or open the editor on a text row. Every change
// applies to the live model and is written straight to config.toml.
func (m model) updateSettings(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc":
		m.mode = modeList
	case "j", "down":
		if m.settingsCur < numSettings-1 {
			m.settingsCur++
		}
	case "k", "up":
		if m.settingsCur > 0 {
			m.settingsCur--
		}
	case "enter":
		switch m.settingsCur {
		case 0, 1, 5, 6:
			m.cycleSetting() // enum/bool rows cycle in place
		default:
			m.prompt(modeSettingEdit, m.settingValue(m.settingsCur), m.settingPlaceholder(m.settingsCur))
		}
	}
	return m, nil
}

// cycleSetting advances an enum row (view or density) and persists.
func (m *model) cycleSetting() {
	switch m.settingsCur {
	case 0: // view: category -> priority -> tag -> table
		m.view = (m.view + 1) % viewMode(viewCount)
		m.resort()
		m.clamp()
		m.saveSettings()
	case 1: // density: compact <-> comfort
		if m.density == comfort {
			m.density = compact
		} else {
			m.density = comfort
		}
		m.saveSettings()
	case 5: // footer: shown <-> hidden
		m.hideFooter = !m.hideFooter
		m.saveSettings()
	case 6: // mouse: off <-> on
		m.mouse = !m.mouse
		m.saveSettings()
	}
}

// settingValue is the editor seed for a text setting row.
func (m model) settingValue(idx int) string {
	switch idx {
	case 2:
		return strconv.Itoa(int(m.autosaveEvery / time.Second))
	case 3:
		return strings.Join(m.categories, ", ")
	case 4:
		return strings.Join(m.statuses, ", ")
	}
	return ""
}

func (m model) settingPlaceholder(idx int) string {
	switch idx {
	case 2:
		return "autosave seconds (0 disables)"
	case 3:
		return "categories, comma-separated"
	case 4:
		return "statuses, comma-separated (done always kept last)"
	}
	return ""
}

// updateSettingEdit runs the shared text input for a text setting row, applying
// and saving on enter.
func (m model) updateSettingEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.mode = modeSettings
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		m.applySettingText(m.settingsCur, v)
		m.saveSettings()
		m.input.Blur()
		m.mode = modeSettings
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// applySettingText writes an edited text setting back onto the live model.
func (m *model) applySettingText(idx int, v string) {
	switch idx {
	case 2:
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.autosaveEvery = time.Duration(n) * time.Second
		}
	case 3:
		m.categories = store.ParseList(v, false)
	case 4:
		m.statuses = normalizeStatuses(store.ParseList(v, true))
	}
}
