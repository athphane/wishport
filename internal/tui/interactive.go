package tui

import (
	"context"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
)

// InteractiveService optionally requests explicit consent and reports connection
// preparation progress. Callbacks may block until the UI handles their message.
type InteractiveService interface {
	Connect(context.Context, inventory.Connection, interaction.Confirm, interaction.Progress) (*exec.Cmd, error)
}

type progressMsg struct {
	generation int
	text       string
}
type consentMsg struct {
	generation int
	consent    interaction.Consent
	reply      chan bool
}

func awaitEvent(ctx context.Context, events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-events:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}

func interactiveCommand(ctx context.Context, generation int, svc InteractiveService, target inventory.Connection, events chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		send := func(msg tea.Msg) bool {
			select {
			case events <- msg:
				return true
			case <-ctx.Done():
				return false
			}
		}
		go func() {
			confirm := func(requestCtx context.Context, consent interaction.Consent) (bool, error) {
				reply := make(chan bool, 1)
				select {
				case events <- consentMsg{generation, consent, reply}:
				case <-ctx.Done():
					return false, ctx.Err()
				case <-requestCtx.Done():
					return false, requestCtx.Err()
				}
				select {
				case yes := <-reply:
					return yes, nil
				case <-ctx.Done():
					return false, ctx.Err()
				case <-requestCtx.Done():
					return false, requestCtx.Err()
				}
			}
			progress := func(text string) { send(progressMsg{generation, text}) }
			command, err := svc.Connect(ctx, target, confirm, progress)
			send(prepared{generation, command, err})
		}()
		return awaitEvent(ctx, events)()
	}
}

func (m *Model) listen() tea.Cmd { return awaitEvent(m.operationCtx, m.events) }
func (m *Model) cancelStatus() string {
	if m.submitted {
		return "Cancelled waiting; installation may still finish on the server"
	}
	return "Cancelled"
}
