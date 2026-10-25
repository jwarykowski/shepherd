package tui

import (
	"fmt"

	binding "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// afterFieldEdit is the mode to land in once a field editor closes: the item
// detail view when the editor was opened from there (mirroring projDirEdit for
// board dirs), else the list. It consumes the flag.
func (m *model) afterFieldEdit() mode {
	if m.fieldEdit {
		m.fieldEdit = false
		return modeDetail
	}
	return modeList
}

// detailFieldKeys are the list's field editors reused from the detail view, so
// the item can be changed while you're looking at it. They're delegated to
// updateList rather than reimplemented — it already guards parent-only fields and
// resorts/re-places the cursor. Keys the detail view handles itself (note, open,
// copy, back, quit) never reach here; delete is deliberately list-only.
var detailFieldKeys = []binding.Binding{
	keys.Edit, keys.Category, keys.Tags, keys.Due, keys.Defer, keys.Link, keys.Prio, keys.Status, keys.Toggle,
}

func (m model) updateDetail(msg fmt.Stringer) (tea.Model, tea.Cmd) {
	ref := m.selRef()
	if ref.item < 0 {
		switch {
		case binding.Matches(msg, keys.Quit):
			return m.quit()
		case binding.Matches(msg, keys.Back):
			m.mode = modeList
		}
		return m, nil
	}
	p := m.rowPtr(ref)
	switch {
	case binding.Matches(msg, keys.Quit):
		return m.quit()
	case binding.Matches(msg, keys.Back):
		m.mode = modeList
	case binding.Matches(msg, keys.Note):
		if !m.global {
			m.mode = modeNote
			m.noteRef = ref
			m.noteEditing = false
			m.note.SetWidth(m.width())
			m.note.SetHeight(noteHeight)
			m.note.SetValue(p.Note)
			m.note.Focus()
		}
	case binding.Matches(msg, keys.Open):
		return m, openLink(p.Link)
	case binding.Matches(msg, keys.Copy):
		return m, copyText(p.Text)
	default:
		if binding.Matches(msg, detailFieldKeys...) && !m.global {
			m.fieldEdit = true // save/cancel comes back here, not to the list
			res, cmd := m.updateList(msg)
			nm := res.(model)
			if !nm.input.Focused() { // an immediate mutation (prio, status): stay put
				nm.mode = modeDetail
				nm.fieldEdit = false
			}
			return nm, cmd
		}
	}
	return m, nil
}

// updateNote drives the multi-line note editor. enter inserts a newline (the
// textarea owns it); edits save live to the item, so esc just closes. One undo
// snapshot is taken on the first change, giving a single entry per edit session.
func (m model) updateNote(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "shift+enter", "ctrl+enter":
		m.note.Blur()
		m.noteEditing = false
		m.mode = modeDetail
		return m, nil
	}
	var cmd tea.Cmd
	m.note, cmd = m.note.Update(msg)
	m.syncNote()
	return m, cmd
}

// syncNote copies the note editor's text onto its item, taking one undo
// snapshot per note session.
func (m *model) syncNote() {
	if ref := m.noteRef; ref.item >= 0 && ref.item < len(m.items) {
		p := m.rowPtr(ref)
		if v := m.note.Value(); v != p.Note { // empty clears
			if !m.noteEditing {
				m.beforeMutate()
				m.noteEditing = true
			}
			p.Note = v
		}
	}
}
