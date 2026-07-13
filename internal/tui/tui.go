// Package tui implements the Bubble Tea TUI for ttl.
//
// Two views are bundled:
//
//	Today  - tasks with due_at <= end of today (open status)
//	Inbox  - all open tasks with no project (root-level)
//
// Keybindings (vim-style):
//
//	j/k or arrows   move selection
//	space           toggle complete
//	n               new task (prompts inline)
//	d               delete (with confirm)
//	r               refresh from server
//	q / ctrl-c      quit
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/model"
)

// View selects the initial filter.
type View string

const (
	ViewToday    View = "today"
	ViewInbox    View = "inbox"
	ViewUpcoming View = "upcoming"
	ViewOverdue  View = "overdue"
	ViewNext     View = "next"
	ViewDone     View = "done"
	ViewTrash    View = "trash"
)

// Run starts the TUI. Blocks until the user quits.
func Run(c *client.Client, view View) error {
	m := newTuiModel(c, view)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// -------------------------- item --------------------------

type tuiItem struct {
	id       string
	title    string
	priority int
	due      *time.Time
	tags     []string
	done     bool
	deleted  bool
}

func (i tuiItem) Title() string {
	if i.done {
		return "[x] " + i.title
	}
	return "[ ] " + i.title
}
func (i tuiItem) Description() string {
	due := ""
	if i.due != nil {
		due = i.due.Format("2006-01-02")
	}
	tags := strings.Join(i.tags, ",")
	if tags != "" {
		tags = "  [" + tags + "]"
	}
	return fmt.Sprintf("%s%s", due, tags)
}
func (i tuiItem) FilterValue() string { return i.title }

// -------------------------- model --------------------------

type tuiMode int

const (
	modeList tuiMode = iota
	modeAdd
	modeEdit
	modeConfirmDelete
)

type tuiModel struct {
	c      *client.Client
	view   View
	list   list.Model
	mode   tuiMode
	input  textinput.Model
	status string
	active *model.TimeEntry
	width  int
	height int
}

type taskMutationMsg struct{ err error }

func newTuiModel(c *client.Client, view View) *tuiModel {
	delegate := list.NewDefaultDelegate()
	l := list.New([]list.Item{}, delegate, 80, 20)
	l.Title = string(view)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	ti := textinput.New()
	ti.CharLimit = 200
	ti.Prompt = "new task> "

	m := &tuiModel{c: c, view: view, list: l, input: ti}
	m.refresh()
	return m
}

func (m *tuiModel) Init() tea.Cmd { return nil }

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeAdd:
		return m.updateAdd(msg)
	case modeEdit:
		return m.updateEdit(msg)
	case modeConfirmDelete:
		return m.updateConfirmDelete(msg)
	default:
		return m.updateList(msg)
	}
}

func (m *tuiModel) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case taskMutationMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		m.refresh()
		m.status = "saved"
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(msg.Width, msg.Height-2)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "n":
			m.mode = modeAdd
			m.input.Focus()
			m.status = "type a task title, enter to save, esc to cancel"
			return m, nil
		case "e":
			if it, ok := m.list.SelectedItem().(tuiItem); ok && !it.deleted {
				m.mode = modeEdit
				m.input.Prompt = "edit title> "
				m.input.SetValue(it.title)
				m.input.Focus()
				return m, nil
			}
		case " ":
			if it, ok := m.list.SelectedItem().(tuiItem); ok && !it.done {
				m.status = "saving..."
				return m, m.completeTaskCmd(it.id)
			}
		case "d":
			if _, ok := m.list.SelectedItem().(tuiItem); ok {
				m.mode = modeConfirmDelete
				m.status = "press y to delete, any other key to cancel"
				return m, nil
			}
		case "r":
			m.refresh()
			m.status = "refreshed"
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7":
			views := map[string]View{"1": ViewInbox, "2": ViewToday, "3": ViewUpcoming, "4": ViewOverdue, "5": ViewNext, "6": ViewDone, "7": ViewTrash}
			m.view = views[msg.String()]
			m.list.Title = string(m.view)
			m.refresh()
			return m, nil
		case "u":
			if it, ok := m.list.SelectedItem().(tuiItem); ok && it.deleted {
				m.status = "restoring..."
				return m, m.restoreTaskCmd(it.id)
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *tuiModel) updateAdd(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.mode = modeList
			m.input.Blur()
			m.input.SetValue("")
			m.status = ""
			return m, nil
		case "enter":
			title := strings.TrimSpace(m.input.Value())
			if title != "" {
				cmd := m.addTaskCmd(title)
				m.mode = modeList
				m.input.Blur()
				m.input.SetValue("")
				m.status = "saving..."
				return m, cmd
			}
			m.mode = modeList
			m.input.Blur()
			m.input.SetValue("")
			m.status = ""
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *tuiModel) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.mode = modeList
			m.input.Blur()
			m.input.SetValue("")
			m.input.Prompt = "new task> "
			return m, nil
		case "enter":
			title := strings.TrimSpace(m.input.Value())
			if it, ok := m.list.SelectedItem().(tuiItem); ok && title != "" {
				m.mode = modeList
				m.input.Blur()
				m.input.SetValue("")
				m.input.Prompt = "new task> "
				m.status = "saving..."
				return m, m.editTaskCmd(it.id, title)
			}
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *tuiModel) updateConfirmDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "y" {
			if it, ok := m.list.SelectedItem().(tuiItem); ok {
				m.mode = modeList
				m.status = "deleting..."
				return m, m.deleteTaskCmd(it.id)
			}
		}
		m.mode = modeList
		m.status = ""
	}
	return m, nil
}

func (m *tuiModel) View() string {
	header := lipgloss.NewStyle().Bold(true).Render(strings.ToUpper(string(m.view)))
	body := m.list.View()
	status := m.status
	if m.mode == modeAdd || m.mode == modeEdit {
		status = m.input.View()
	}
	active := m.activeLine()
	return fmt.Sprintf("%s\n%s\n%s\n%s", header, active, body, status)
}

// activeLine renders the running-timer indicator if any.
func (m *tuiModel) activeLine() string {
	e := m.active
	if e == nil {
		return ""
	}
	elapsed := time.Since(e.StartedAt).Round(time.Second)
	title := e.TaskTitle
	if title == "" {
		title = "(no task)"
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")).Bold(true)
	if e.Kind == "pomodoro" {
		return style.Render(fmt.Sprintf("[pomodoro] %s  %s", title, elapsed))
	}
	return style.Render(fmt.Sprintf("[tracking] %s  %s", title, elapsed))
}

// -------------------------- async actions --------------------------

func (m *tuiModel) refresh() {
	opts := client.ListOpts{Limit: 500, View: string(m.view)}
	tasks, err := m.c.ListTasks(context.Background(), opts)
	if err != nil {
		m.status = "error: " + err.Error()
		return
	}
	items := make([]list.Item, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, tuiItem{
			id:       t.ID,
			title:    t.Title,
			priority: t.Priority,
			due:      t.DueAt,
			tags:     t.Tags,
			deleted:  t.DeletedAt != nil,
			done:     t.Status == "done",
		})
	}
	m.list.SetItems(items)
	m.active, _ = m.c.ActiveTimer(context.Background())
}

func (m *tuiModel) completeTaskCmd(id string) tea.Cmd {
	return func() tea.Msg { _, err := m.c.CompleteTask(context.Background(), id); return taskMutationMsg{err: err} }
}

func (m *tuiModel) deleteTaskCmd(id string) tea.Cmd {
	return func() tea.Msg { return taskMutationMsg{err: m.c.DeleteTask(context.Background(), id)} }
}

func (m *tuiModel) restoreTaskCmd(id string) tea.Cmd {
	return func() tea.Msg { _, err := m.c.RestoreTask(context.Background(), id); return taskMutationMsg{err: err} }
}

func (m *tuiModel) addTaskCmd(title string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.c.CreateTask(context.Background(), client.CreateTaskOpts{Title: title, Priority: 0})
		return taskMutationMsg{err: err}
	}
}

func (m *tuiModel) editTaskCmd(id, title string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.c.UpdateTask(context.Background(), id, map[string]any{"title": title})
		return taskMutationMsg{err: err}
	}
}
