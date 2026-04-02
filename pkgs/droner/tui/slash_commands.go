package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

type slashCommand struct {
	Name        string
	Description string
}

func loadGlobalOpencodeSlashCommands() ([]slashCommand, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return loadSlashCommandsFromDir(filepath.Join(homeDir, ".config", "opencode", "commands"))
}

func loadSlashCommandsFromDir(dir string) ([]slashCommand, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	commands := make([]slashCommand, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.TrimSpace(name) == "" {
			continue
		}
		description := ""
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err == nil {
			description = parseSlashCommandDescription(string(contents))
		}
		commands = append(commands, slashCommand{Name: name, Description: description})
	}
	sort.Slice(commands, func(i int, j int) bool {
		return commands[i].Name < commands[j].Name
	})
	return commands, nil
}

func parseSlashCommandDescription(contents string) string {
	trimmed := strings.TrimSpace(contents)
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			return ""
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "description" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lookupSlashCommand(rawInput string, commands []slashCommand) (slashCommand, bool) {
	name, _, ok := splitLeadingSlashCommand(rawInput)
	if !ok {
		return slashCommand{}, false
	}
	for _, command := range commands {
		if command.Name == name {
			return command, true
		}
	}
	return slashCommand{}, false
}

func splitLeadingSlashCommand(rawInput string) (name string, arguments string, ok bool) {
	if !strings.HasPrefix(rawInput, "/") {
		return "", "", false
	}
	runes := []rune(rawInput)
	end := 1
	for end < len(runes) && runes[end] != ' ' && runes[end] != '\t' && runes[end] != '\n' {
		end++
	}
	if end <= 1 {
		return "", "", false
	}
	name = string(runes[1:end])
	if end == len(runes) {
		return name, "", true
	}
	arguments = string(runes[end:])
	if arguments != "" {
		arguments = string([]rune(arguments)[1:])
	}
	return name, arguments, true
}

func commandInvocationFromPrompt(rawInput string, prompt *messages.Message, commands []slashCommand) *messages.CommandInvocation {
	command, ok := lookupSlashCommand(rawInput, commands)
	if !ok {
		return nil
	}
	_, arguments, _ := splitLeadingSlashCommand(rawInput)
	parts := make([]messages.MessagePart, 0)
	if prompt != nil {
		for _, part := range prompt.Parts {
			if part.Type != messages.PartTypeFile {
				continue
			}
			parts = append(parts, part)
		}
	}
	return &messages.CommandInvocation{
		Name:      command.Name,
		Arguments: arguments,
		Parts:     parts,
	}
}
