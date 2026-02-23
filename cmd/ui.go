package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/amaliebjorgen/fabricant/pkg/auth"
	"github.com/amaliebjorgen/fabricant/pkg/devops"
	"github.com/amaliebjorgen/fabricant/pkg/fabric"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StartUI launches the interactive Terminal User Interface for Fabricant.
func StartUI() {
	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error starting UI: %v\n", err)
		os.Exit(1)
	}
}

// ----- Styles -----
var (
	titleStyle   = lipgloss.NewStyle().MarginLeft(2)
	itemStyle    = lipgloss.NewStyle().PaddingLeft(4)
	quitStyle    = lipgloss.NewStyle().Margin(1, 0, 0, 2).Foreground(lipgloss.Color("241"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(1, 2)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Padding(1, 2)
)

// ----- Model States -----
type sessionState int

const (
	stateInit sessionState = iota
	stateLoadingWorkspaces
	stateSelectWorkspace
	stateLoadingGit
	stateEnterBranch
	stateEnterWorkspace
	stateExecuting
	stateDone
	stateError
)

type model struct {
	state          sessionState
	err            error
	successMsg     string
	executionInfos []string

	authClient   *auth.Authenticator
	fabricClient *fabric.Client
	devopsClient *devops.Client

	// UI Components
	spinner      spinner.Model
	workspaceLst list.Model
	branchInput  textinput.Model
	wsInput      textinput.Model

	// Data
	workspaces           []fabric.Workspace
	selectedDevWorkspace *fabric.Workspace
	newBranchName        string
	newWorkspaceName     string
}

func initialModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	bi := textinput.New()
	bi.Placeholder = "feature/my-new-branch"
	bi.Focus()
	bi.CharLimit = 100

	wsi := textinput.New()
	wsi.Placeholder = "My Feature Workspace"
	wsi.CharLimit = 100

	lst := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	lst.Title = "Select Parent Dev Workspace"
	lst.SetShowStatusBar(false)

	return model{
		state:        stateInit,
		spinner:      s,
		workspaceLst: lst,
		branchInput:  bi,
		wsInput:      wsi,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, initClientsCmd)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.workspaceLst.SetSize(msg.Width-h, msg.Height-v)
	case errMsg:
		m.err = msg.err
		m.state = stateError
		return m, nil
	case clientsReadyMsg:
		m.authClient = msg.auth
		m.fabricClient = msg.fabric
		m.devopsClient = msg.devops
		m.state = stateLoadingWorkspaces
		return m, m.fetchWorkspacesCmd
	case workspacesMsg:
		m.workspaces = msg.workspaces
		items := make([]list.Item, len(m.workspaces))
		for i, w := range m.workspaces {
			items[i] = workspaceItem{w}
		}
		m.workspaceLst.SetItems(items)
		m.state = stateSelectWorkspace
		return m, nil
	case gitConnectionMsg:
		if msg.details == nil || msg.details.GitProviderType == "" {
			m.err = fmt.Errorf("selected workspace does not have git integration (or unsupported provider)")
			m.state = stateError
			return m, nil
		}
		m.selectedDevWorkspace.GitProviderDetails = msg.details
		m.state = stateEnterBranch
		return m, textinput.Blink
	case executionStepMsg:
		m.executionInfos = append(m.executionInfos, msg.info)
		return m, nil
	case executionDoneMsg:
		m.successMsg = msg.msg
		m.state = stateDone
		return m, tea.Quit
	}

	// State-specific updates
	switch m.state {
	case stateInit, stateLoadingWorkspaces, stateLoadingGit, stateExecuting:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case stateSelectWorkspace:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				if i, ok := m.workspaceLst.SelectedItem().(workspaceItem); ok {
					m.selectedDevWorkspace = &i.workspace
					m.state = stateLoadingGit
					return m, m.fetchGitConnectionCmd(m.selectedDevWorkspace.Id)
				}
			}
		}
		m.workspaceLst, cmd = m.workspaceLst.Update(msg)
		cmds = append(cmds, cmd)

	case stateEnterBranch:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				m.newBranchName = m.branchInput.Value()
				if m.newBranchName != "" {
					m.state = stateEnterWorkspace
					m.wsInput.SetValue("Feature - " + m.newBranchName)
					m.wsInput.Focus()
					return m, textinput.Blink
				}
			}
		}
		m.branchInput, cmd = m.branchInput.Update(msg)
		cmds = append(cmds, cmd)

	case stateEnterWorkspace:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				m.newWorkspaceName = m.wsInput.Value()
				if m.newWorkspaceName != "" {
					m.state = stateExecuting
					return m, m.executeFlowCmd
				}
			}
		}
		m.wsInput, cmd = m.wsInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.state == stateError {
		return errorStyle.Render(fmt.Sprintf("\nError: %v\n\nPress ctrl+c to exit.", m.err))
	}

	switch m.state {
	case stateInit:
		return fmt.Sprintf("\n %s Initializing clients...\n", m.spinner.View())
	case stateLoadingWorkspaces:
		return fmt.Sprintf("\n %s Loading workspaces from Fabric...\n", m.spinner.View())
	case stateLoadingGit:
		return fmt.Sprintf("\n %s Checking Git configuration...\n", m.spinner.View())
	case stateSelectWorkspace:
		return "\n" + m.workspaceLst.View()
	case stateEnterBranch:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			"\n  Enter new feature branch name:",
			"  "+m.branchInput.View(),
			quitStyle.Render("Press Enter to continue, or ctrl+c to quit."),
		)
	case stateEnterWorkspace:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			"\n  Enter new Fabric workspace name:",
			"  "+m.wsInput.View(),
			quitStyle.Render("Press Enter to execute, or ctrl+c to quit."),
		)
	case stateExecuting:
		logs := strings.Join(m.executionInfos, "\n  ")
		return fmt.Sprintf("\n %s Executing Workflow...\n\n  %s\n", m.spinner.View(), logs)
	case stateDone:
		return successStyle.Render(fmt.Sprintf("\nSuccess!\n%s\n", m.successMsg))
	}

	return ""
}

// ----- Commands (Side Effects) -----

type errMsg struct{ err error }

type clientsReadyMsg struct {
	auth   *auth.Authenticator
	fabric *fabric.Client
	devops *devops.Client
}

type workspacesMsg struct{ workspaces []fabric.Workspace }
type gitConnectionMsg struct{ details *fabric.GitProviderDetails }
type executionStepMsg struct{ info string }
type executionDoneMsg struct{ msg string }

func initClientsCmd() tea.Msg {
	a, err := auth.NewAuthenticator()
	if err != nil {
		return errMsg{err}
	}
	return clientsReadyMsg{
		auth:   a,
		fabric: fabric.NewClient(a),
		devops: devops.NewClient(a),
	}
}

func (m model) fetchWorkspacesCmd() tea.Msg {
	ctx := context.Background()
	ws, err := m.fabricClient.ListWorkspaces(ctx)
	if err != nil {
		return errMsg{err}
	}
	return workspacesMsg{workspaces: ws}
}

func (m model) fetchGitConnectionCmd(id string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		resp, err := m.fabricClient.GetGitConnection(ctx, id)
		if err != nil {
			return errMsg{fmt.Errorf("failed to get git connection: %w", err)}
		}
		return gitConnectionMsg{details: resp.GitProviderDetails}
	}
}

func (m model) executeFlowCmd() tea.Msg {
	ctx := context.Background()
	gitInfo := m.selectedDevWorkspace.GitProviderDetails

	// 1. Get Base Commit ID
	baseCommitId, err := m.devopsClient.GetBranchObjectId(ctx, gitInfo.OrganizationName, gitInfo.ProjectName, gitInfo.RepositoryName, gitInfo.BranchName)
	if err != nil {
		return errMsg{fmt.Errorf("getting dev branch commit: %w", err)}
	}

	// Wait, bubble tea cmd cannot yield multiple tea.Msgs directly via channel easily without returning a Cmd per message. Let's just do it sequentially or send msgs using another method.
	// We'll just do it all here for now and report success at the end, returning errMsg if anything fails.

	// Create Branch
	err = m.devopsClient.CreateBranch(ctx, gitInfo.OrganizationName, gitInfo.ProjectName, gitInfo.RepositoryName, m.newBranchName, baseCommitId)
	if err != nil {
		return errMsg{fmt.Errorf("creating feature branch: %w", err)}
	}

	// Create Workspace
	req := fabric.CreateWorkspaceRequest{
		DisplayName: m.newWorkspaceName,
		Description: "Feature workspace for " + m.newBranchName + " (Parent: " + m.selectedDevWorkspace.DisplayName + ")",
		CapacityId:  m.selectedDevWorkspace.CapacityId,
	}
	newWs, err := m.fabricClient.CreateWorkspace(ctx, req)
	if err != nil {
		return errMsg{fmt.Errorf("creating workspace: %w", err)}
	}

	// Connect to Git
	newGitInfo := *gitInfo
	newGitInfo.BranchName = m.newBranchName
	err = m.fabricClient.ConnectWorkspaceToGit(ctx, newWs.Id, fabric.ConnectToGitRequest{GitProviderDetails: &newGitInfo})
	if err != nil {
		return errMsg{fmt.Errorf("connecting git: %w", err)}
	}

	// Update from Git
	err = m.fabricClient.UpdateWorkspaceFromGit(ctx, newWs.Id)
	if err != nil {
		return errMsg{fmt.Errorf("updating from git: %w", err)}
	}

	// Update Connections
	// err = m.fabricClient.UpdateConnections(ctx, newWs.Id, nil)

	return executionDoneMsg{"Workspace and Branch created and synced successfully!"}
}

// ----- Helps -----
type workspaceItem struct {
	workspace fabric.Workspace
}

func (i workspaceItem) Title() string       { return i.workspace.DisplayName }
func (i workspaceItem) Description() string { return i.workspace.Id }
func (i workspaceItem) FilterValue() string { return i.workspace.DisplayName }
