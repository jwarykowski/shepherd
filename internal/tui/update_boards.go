package tui

import (
	"shepherd/internal/store"

	tea "charm.land/bubbletea/v2"
)

// enterBoards opens the board picker, listing every board with the current
// one (default when unnamed) under the cursor.
func (m *model) enterBoards() {
	m.setBoards(store.Boards())
	m.projCur = 0
	m.projArchived = false
	m.projNotice = ""
	cur := m.board
	if cur == "" {
		cur = "default"
	}
	for i, b := range m.projRows {
		if b.Name == cur {
			m.projCur = i
			break
		}
	}
	m.mode = modeBoards
}

// liveOnlyKeys are the picker actions that need a board sitting in its normal
// place — inert in the archived view.
var liveOnlyKeys = map[string]bool{"enter": true, "d": true, "a": true, "r": true, "x": true, "A": true}

// updateBoards handles keys in the board picker: navigate, enter to jump to
// the selected board (flushing any unsaved edits first), esc to return.
func (m model) updateBoards(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	// A stashed board has no file in place, so the archived view is unarchive-only:
	// nothing to open, inspect, rename, archive or delete.
	if m.projArchived && liveOnlyKeys[key] {
		return m, nil
	}
	switch key {
	case "q":
		return m.quit()
	case "esc":
		m.mode = modeList
	case "j", "down":
		if m.projCur < len(m.projRows)-1 {
			m.projCur++
		}
	case "k", "up":
		if m.projCur > 0 {
			m.projCur--
		}
	case "e": // toggle between live and archived boards
		m.projArchived = !m.projArchived
		m.projNotice = ""
		m.projCur = 0
		if m.projArchived {
			m.setBoards(store.ArchivedBoards())
		} else {
			m.setBoards(store.Boards())
		}
	case "u": // unarchive the selected board (archived view only)
		if !m.projArchived {
			break
		}
		if b := m.selectedBoard(); b != nil {
			if err := store.UnarchiveBoard(b.Name); err != nil {
				m.projNotice = err.Error()
			} else {
				m.projArchived = false
				m.setBoards(store.Boards())
				for i, x := range m.projRows { // land on the restored board
					if x.Name == b.Name {
						m.projCur = i
						break
					}
				}
			}
		}
	case "enter":
		if len(m.projRows) == 0 {
			m.mode = modeList
			return m, nil
		}
		name := m.projRows[m.projCur].Name
		proj := name
		if name == "default" {
			proj = ""
		}
		if !m.global && m.dirty {
			m.save()
		}
		nm := newModel(proj) // jump: rebuild the board fresh, like toggleGlobal
		nm.adoptTerminal(m)
		nm.density, nm.hideFooter = m.density, m.hideFooter
		nm.clamp()
		return nm, nil
	case "d": // detail view for the selected board (name, dir, paths, counts)
		if m.selectedBoard() != nil {
			m.projNotice = ""
			m.mode = modeBoardDetail
		}
	case "a": // create a new board
		m.projNotice = ""
		m.prompt(modeBoardNew, "", "new board name")
	case "r": // rename the selected board (not the default)
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			m.projNotice = ""
			m.prompt(modeBoardRename, b.Name, "new board name")
		}
	case "x": // delete the selected board (confirmed)
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			m.mode = modeConfirmDelete
		}
	case "A": // archive the selected board into boards/archived/
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			m.saveBeforeMove(b.Name)
			if err := store.ArchiveBoard(b.Name); err != nil {
				m.projNotice = err.Error()
			} else {
				return m.afterBoardChange(b.Name, ""), nil
			}
		}
	}
	return m, nil
}

// updateBoardDetail handles the board detail screen: e opens the dir editor
// for this board (reusing modeBoardDir), esc returns to the picker.
func (m model) updateBoardDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc":
		m.mode = modeBoards
	case "e": // edit this board's working dir
		if b := m.selectedBoard(); b != nil {
			m.projPending = b.Name
			m.projDirEdit = true
			m.prompt(modeBoardDir, store.BoardDir(b.Name), "working dir (empty to clear)")
		}
	}
	return m, nil
}

// selectedBoard is the board row under the picker cursor, or nil when empty.
func (m model) selectedBoard() *store.Board {
	if m.projCur < 0 || m.projCur >= len(m.projRows) {
		return nil
	}
	return &m.projRows[m.projCur]
}

// currentBoardName is the name of the board currently open ("default" when the
// unnamed board).
func (m model) currentBoardName() string {
	if m.board == "" {
		return "default"
	}
	return m.board
}

// saveBeforeMove flushes unsaved edits when the open board is about to be
// renamed or archived, since afterBoardChange reopens it from the moved file.
func (m *model) saveBeforeMove(name string) {
	if !m.global && m.dirty && name == m.currentBoardName() {
		m.save()
	}
}

// afterBoardChange refreshes the picker after a rename/delete/archive. If the
// board that changed is the one currently open, it re-opens that board under its
// new name (newName), or the default board when it was removed/archived
// (newName == ""), so the live model never points at a moved file.
func (m model) afterBoardChange(oldName, newName string) model {
	if oldName == m.currentBoardName() {
		proj := newName
		if proj == "default" {
			proj = ""
		}
		nm := newModel(proj)
		nm.adoptTerminal(m)
		nm.density, nm.hideFooter = m.density, m.hideFooter
		nm.enterBoards()
		return nm
	}
	m.setBoards(store.Boards())
	if m.projCur >= len(m.projRows) {
		m.projCur = len(m.projRows) - 1
	}
	if m.projCur < 0 {
		m.projCur = 0
	}
	m.mode = modeBoards
	return m
}

// updateConfirmDelete handles the delete-board confirmation: y deletes the
// selected board, anything else cancels back to the picker.
func (m model) updateConfirmDelete(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "y", "Y":
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			if err := store.DeleteBoard(b.Name); err == nil {
				return m.afterBoardChange(b.Name, ""), nil
			}
		}
		m.mode = modeBoards
	default:
		m.mode = modeBoards
	}
	return m, nil
}
