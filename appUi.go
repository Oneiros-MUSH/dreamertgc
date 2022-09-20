package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"os"
	"strings"
)

type AppStage int

const (
	Starting AppStage = iota
	ConnectingToServer
	LoadingInitialData
	Ready
)

type ErrorState int

const (
	ServerConnectionTimeout ErrorState = iota
	UnknownError
	FatalError
	NoError
)

type AppMainModel struct {
	stage         AppStage
	errorState    ErrorState
	rcv           ServerEventReceiver
	altWindow     AltWindow
	infoPane      CharacterPane
	primaryPane   ChatPane
	secondaryPane SystemPane
	statusBar     StatusBar
}

type AltWindow struct {
	IsFocused bool
	Contents  []string
}

func (scr *AppMainModel) Init() tea.Cmd {
	return RunSignalRClient(&scr.rcv)
}

func (scr *AppMainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case ServerConnectionEstablishedMsg:
		scr.altWindow.Contents = append(scr.altWindow.Contents, "Connected!")
		scr.altWindow.IsFocused = !scr.altWindow.IsFocused
		tea.ExitAltScreen()
		scr.primaryPane.IsFocused = true
		scr.primaryPane.ChatInput.Blink()
		scr.primaryPane.ChatInput.Focus()
		return scr, scr.Listen(scr.rcv.UiUpdateChannel) // needs to kick off some sort of listener for incoming signalR invokes

	case ServerDataReceivedMsg:
		return scr, scr.Listen(scr.rcv.UiUpdateChannel)

	case ErrMsg:
		switch msg.ErrType {

		case ServerConnectionTimeout:
			scr.errorState = ServerConnectionTimeout
			scr.altWindow.Contents = append(scr.altWindow.Contents, "Server Connection Failed. Retry? (Y/N)")
			return scr, nil

		case FatalError:
			return scr, tea.Quit

		}

	case tea.KeyMsg:
		// altscreen keystroke handlers
		if scr.altWindow.IsFocused {
			if scr.errorState == ServerConnectionTimeout {
				switch msg.Type {
				case tea.KeyRunes:
					switch string(msg.Runes) {
					case "y":
						scr.errorState = NoError
						return scr, RunSignalRClient(&scr.rcv)
					case "n":
						return scr, tea.Quit
					}
				}
			}
		}

		// main screen keystroke handlers
		if !scr.altWindow.IsFocused {
			switch msg.Type {

			case tea.KeyEnter:
				if !scr.primaryPane.ChatInput.Focused() {
					scr.primaryPane.ChatInput.Focus()
				}
				if scr.primaryPane.ChatInput.Focused() && len(scr.primaryPane.ChatInput.Value()) > 0 {
					scr.secondaryPane.Contents = append(scr.secondaryPane.Contents, fmt.Sprintf("Sent msg: %v\n", scr.primaryPane.ChatInput.Value()))
					scr.primaryPane.ChatInput.Reset()
				}
			}

			scr.primaryPane.ChatInput, _ = scr.primaryPane.ChatInput.Update(msg)
		}

		// universal keystroke handlers
		switch msg.Type {
		case tea.KeyF6: // toggle debug screen
			scr.altWindow.IsFocused = !scr.altWindow.IsFocused

		case tea.KeyCtrlD: // dump current state to debug screen
			scr.altWindow.Contents = append(scr.altWindow.Contents, fmt.Sprintf("chatFocused: %v \nchatContents: %v", scr.primaryPane.ChatInput.Focused(), scr.primaryPane.ChatInput.Value()))

		case tea.KeyCtrlQ: // exit program
			return scr, tea.Quit
		}
	}
	return scr, nil
}

func (scr *AppMainModel) View() string {
	if scr.altWindow.IsFocused {
		return RenderAltView(scr)
	} else {
		return RenderMainView(scr)
	}
}

func RenderMainView(scr *AppMainModel) string {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	rightStack := lipgloss.JoinVertical(lipgloss.Right, scr.primaryPane.RenderChatPane(w, h), scr.secondaryPane.RenderCommandPane(w, h))
	mainApp := lipgloss.JoinHorizontal(lipgloss.Top, scr.infoPane.RenderInfoPane(w, h), rightStack)

	return mainApp + "\n" + scr.statusBar.RenderStatusBar(w)
}

func RenderAltView(scr *AppMainModel) string {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	mainStyle := lipgloss.NewStyle().
		Width(w-2).Height(int(h-3)).Border(lipgloss.DoubleBorder(), true)

	mainApp := mainStyle.Render(strings.Join(scr.altWindow.Contents, "\n"))

	return mainApp + "\n" + scr.statusBar.RenderStatusBar(w)
}

type ChatPane struct {
	IsFocused bool
	Contents  []string
	ChatInput textinput.Model
}

func (pp *ChatPane) RenderChatPane(w int, h int) string {
	style := lipgloss.NewStyle().
		Width(int((w/3)*2)-1).Height(int(((2*h)/3)-2)).Border(lipgloss.DoubleBorder(), true)

	chatHistory := viewport.New(int((w/3)*2)-1, int(((2*h)/3)-3))
	chatHistory.SetContent(strings.Join(pp.Contents, "\n"))

	pp.ChatInput.TextStyle = lipgloss.NewStyle().Background(lipgloss.Color("#AFAFAF")).Foreground(lipgloss.Color("#000000"))
	//pp.ChatInput.BackgroundStyle = lipgloss.NewStyle().Background(lipgloss.Color("#AFAFAF")).Foreground(lipgloss.Color("#000000"))
	//pp.ChatInput.PlaceholderStyle = lipgloss.NewStyle().Background(lipgloss.Color("#AFAFAF")).Foreground(lipgloss.Color("#000000"))
	pp.ChatInput.Width = int((w/3)*2) - 5
	pp.ChatInput.CharLimit = 255
	//pp.ChatInput.Placeholder = "Chat Goes Here..."

	return style.Render(chatHistory.View() + "\n" + pp.ChatInput.View())
}

type SystemPane struct {
	IsFocused    bool
	Contents     []string
	commandInput textinput.Model
}

func (sp *SystemPane) RenderCommandPane(w int, h int) string {
	style := lipgloss.NewStyle().
		Width(int((w/3)*2)-1).Height(int((h/3)-2)).Border(lipgloss.DoubleBorder(), true)

	return style.Render(strings.Join(sp.Contents, "\n"))
}

type StatusBar struct {
	LeftBlurb    string
	MiddleString string
	RightBlurb   string
}

func (sb *StatusBar) RenderStatusBar(w int) string {
	var statusBarLeftChunk = lipgloss.NewStyle().Background(lipgloss.Color("#FF5F87")).Foreground(lipgloss.Color("#FFFDF5")).Align(lipgloss.Left)
	var statusBarRightChunk = lipgloss.NewStyle().Background(lipgloss.Color("#FF5F87")).Foreground(lipgloss.Color("#FFFDF5")).Align(lipgloss.Right)

	left := statusBarLeftChunk.Render(sb.LeftBlurb)
	right := statusBarRightChunk.Render(sb.RightBlurb)

	var middleText = lipgloss.NewStyle().Background(lipgloss.Color("#AFAFAF")).Foreground(lipgloss.Color("#000000")).Align(lipgloss.Center).Width(w - lipgloss.Width(left) - lipgloss.Width(right))
	middle := middleText.Render(sb.MiddleString)

	statusBarPrerender := lipgloss.JoinHorizontal(lipgloss.Top, left, middle, right)
	return statusBarPrerender
}
