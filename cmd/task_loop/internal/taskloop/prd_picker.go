package taskloop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

const (
	maximumPRDViewport        = 10
	escapeSequenceWaitTimeout = 100 * time.Millisecond
)

type terminalPRDSelector struct {
	input  *os.File
	output *prdPickerTerminalOutput
}

type terminalPRDInput struct {
	file *os.File
}

type prdPickerOutputOpener func() (*prdPickerTerminalOutput, error)

type prdPickerTerminalOutput struct {
	file      *os.File
	restore   func() error
	closeOnce sync.Once
	closeErr  error
}

func systemPRDSelector() (PRDSelector, bool) {
	input, ok := parseTerminalPRDInput(os.Stdin)
	if !ok {
		return nil, false
	}
	return openTerminalPRDSelector(input, openPRDPickerTerminalOutput)
}

func parseTerminalPRDInput(input *os.File) (terminalPRDInput, bool) {
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return terminalPRDInput{}, false
	}
	return terminalPRDInput{file: input}, true
}

func openTerminalPRDSelector(input terminalPRDInput, openOutput prdPickerOutputOpener) (PRDSelector, bool) {
	if openOutput == nil {
		return nil, false
	}
	output, err := openOutput()
	if err != nil || output == nil || output.file == nil {
		if output != nil {
			_ = output.Close()
		}
		return nil, false
	}
	return &terminalPRDSelector{input: input.file, output: output}, true
}

func newPRDPickerTerminalOutput(file *os.File, restore func() error) *prdPickerTerminalOutput {
	if restore == nil {
		restore = func() error { return nil }
	}
	return &prdPickerTerminalOutput{file: file, restore: restore}
}

func (output *prdPickerTerminalOutput) Close() error {
	if output == nil {
		return nil
	}
	output.closeOnce.Do(func() {
		var restoreErr, fileErr error
		if output.restore != nil {
			restoreErr = output.restore()
		}
		if output.file != nil {
			fileErr = output.file.Close()
		}
		output.closeErr = errors.Join(restoreErr, fileErr)
	})
	return output.closeErr
}

func (selector *terminalPRDSelector) Select(ctx context.Context, options []PRDOption) (selected PRDOption, ok bool, err error) {
	defer func() {
		if closeErr := selector.output.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close terminal output after PRD selection: %w", closeErr))
		}
	}()

	inputFD := int(selector.input.Fd())
	previousState, err := term.MakeRaw(inputFD)
	if err != nil {
		return PRDOption{}, false, fmt.Errorf("enable keyboard PRD selection: %w", err)
	}
	defer func() {
		if restoreErr := term.Restore(inputFD, previousState); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore terminal after PRD selection: %w", restoreErr))
		}
	}()

	width, height, sizeErr := term.GetSize(int(selector.output.file.Fd()))
	if sizeErr != nil {
		width, height = 80, 24
	}
	picker := keyboardPRDPicker{
		input:  selector.input,
		output: selector.output.file,
		width:  width,
		height: height,
	}
	return picker.Select(ctx, options)
}

type keyboardPRDPicker struct {
	input  io.Reader
	output io.Writer
	width  int
	height int
}

func (picker keyboardPRDPicker) Select(ctx context.Context, options []PRDOption) (PRDOption, bool, error) {
	if len(options) == 0 {
		return PRDOption{}, false, nil
	}

	state := newPRDPickerState(len(options), pickerViewport(picker.height, len(options)))
	if _, err := io.WriteString(picker.output, "\x1b[?25l"); err != nil {
		return PRDOption{}, false, err
	}
	cursorHidden := true
	defer func() {
		if cursorHidden {
			_, _ = io.WriteString(picker.output, "\x1b[?25h")
		}
	}()

	renderedLines := state.viewport + 2
	if err := renderPRDPicker(picker.output, picker.width, options, state, false); err != nil {
		return PRDOption{}, false, err
	}

	keys := pickerKeyReader{reader: picker.input}
	if inputFile, ok := picker.input.(*os.File); ok && term.IsTerminal(int(inputFile.Fd())) {
		keys.waitForInput = func(timeout time.Duration) (bool, error) {
			return waitForPRDPickerInput(inputFile, timeout)
		}
		keys.waitBeforeRead = true
	}
	for {
		key, err := keys.next(ctx)
		if errors.Is(err, io.EOF) {
			_ = clearPRDPicker(picker.output, renderedLines)
			return PRDOption{}, false, nil
		}
		if err != nil {
			_ = clearPRDPicker(picker.output, renderedLines)
			return PRDOption{}, false, fmt.Errorf("read PRD selection: %w", err)
		}

		switch key {
		case pickerKeySelect:
			if err := clearPRDPicker(picker.output, renderedLines); err != nil {
				return PRDOption{}, false, err
			}
			if _, err := io.WriteString(picker.output, "\x1b[?25h"); err != nil {
				return PRDOption{}, false, err
			}
			cursorHidden = false
			return options[state.selected], true, nil
		case pickerKeyCancel:
			if err := clearPRDPicker(picker.output, renderedLines); err != nil {
				return PRDOption{}, false, err
			}
			return PRDOption{}, false, nil
		case pickerKeyUp, pickerKeyDown, pickerKeyPageUp, pickerKeyPageDown, pickerKeyHome, pickerKeyEnd:
			state = state.apply(key)
			if err := renderPRDPicker(picker.output, picker.width, options, state, true); err != nil {
				return PRDOption{}, false, err
			}
		}
	}
}

type prdPickerState struct {
	count    int
	viewport int
	selected int
	offset   int
}

func newPRDPickerState(count, viewport int) prdPickerState {
	if count <= 0 {
		return prdPickerState{}
	}
	viewport = clamp(viewport, 1, count)
	return prdPickerState{count: count, viewport: viewport}
}

func (state prdPickerState) apply(key pickerKey) prdPickerState {
	if state.count == 0 {
		return state
	}
	next := state.selected
	switch key {
	case pickerKeyUp:
		next--
	case pickerKeyDown:
		next++
	case pickerKeyPageUp:
		next -= state.viewport
	case pickerKeyPageDown:
		next += state.viewport
	case pickerKeyHome:
		next = 0
	case pickerKeyEnd:
		next = state.count - 1
	}
	state.selected = clamp(next, 0, state.count-1)
	if state.selected < state.offset {
		state.offset = state.selected
	}
	if state.selected >= state.offset+state.viewport {
		state.offset = state.selected - state.viewport + 1
	}
	return state
}

func pickerViewport(height, optionCount int) int {
	available := height - 3
	if available < 1 {
		available = 1
	}
	if available > maximumPRDViewport {
		available = maximumPRDViewport
	}
	if available > optionCount {
		return optionCount
	}
	return available
}

func renderPRDPicker(writer io.Writer, width int, options []PRDOption, state prdPickerState, replace bool) error {
	var screen strings.Builder
	if replace {
		fmt.Fprintf(&screen, "\x1b[%dA", state.viewport+2)
	}
	lineWidth := 0
	if width > 1 {
		lineWidth = width - 1
	}
	lines := make([]string, 0, state.viewport+2)
	lines = append(lines, truncatePRDPickerLine("Select a PRD (↑/↓, PgUp/PgDn, Enter; Esc cancels)", lineWidth))
	for index := state.offset; index < state.offset+state.viewport; index++ {
		prefix := "  "
		if index == state.selected {
			prefix = "> "
		}
		label := sanitizePRDPickerLabel(options[index].Label)
		lines = append(lines, truncatePRDPickerLine(prefix+label, lineWidth))
	}
	status := fmt.Sprintf("%d/%d", state.selected+1, state.count)
	if state.offset > 0 {
		status = "↑ more  " + status
	}
	if state.offset+state.viewport < state.count {
		status += "  ↓ more"
	}
	lines = append(lines, truncatePRDPickerLine(status, lineWidth))
	for _, line := range lines {
		screen.WriteString("\r\x1b[2K")
		screen.WriteString(line)
		screen.WriteByte('\n')
	}
	_, err := io.WriteString(writer, screen.String())
	return err
}

func clearPRDPicker(writer io.Writer, renderedLines int) error {
	var sequence strings.Builder
	fmt.Fprintf(&sequence, "\x1b[%dA", renderedLines)
	for index := 0; index < renderedLines; index++ {
		sequence.WriteString("\r\x1b[2K")
		if index+1 < renderedLines {
			sequence.WriteByte('\n')
		}
	}
	if renderedLines > 1 {
		fmt.Fprintf(&sequence, "\x1b[%dA", renderedLines-1)
	}
	sequence.WriteByte('\r')
	_, err := io.WriteString(writer, sequence.String())
	return err
}

func truncatePRDPickerLine(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(value) <= width {
		return value
	}
	ellipsis := "…"
	available := width - uniseg.StringWidth(ellipsis)
	if available <= 0 {
		return "…"
	}

	var truncated strings.Builder
	used := 0
	state := -1
	for len(value) > 0 {
		cluster, rest, clusterWidth, nextState := uniseg.FirstGraphemeClusterInString(value, state)
		if used+clusterWidth > available {
			break
		}
		truncated.WriteString(cluster)
		used += clusterWidth
		value = rest
		state = nextState
	}
	truncated.WriteString(ellipsis)
	return truncated.String()
}

func sanitizePRDPickerLabel(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return '�'
		}
		return character
	}, value)
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

type pickerKey uint8

const (
	pickerKeyUnknown pickerKey = iota
	pickerKeyUp
	pickerKeyDown
	pickerKeyPageUp
	pickerKeyPageDown
	pickerKeyHome
	pickerKeyEnd
	pickerKeySelect
	pickerKeyCancel
)

type pickerKeyReader struct {
	reader         io.Reader
	pending        []byte
	readErr        error
	waitForInput   func(time.Duration) (bool, error)
	waitBeforeRead bool
}

func (reader *pickerKeyReader) next(ctx context.Context) (pickerKey, error) {
	for {
		if err := ctx.Err(); err != nil {
			return pickerKeyUnknown, err
		}
		if len(reader.pending) == 0 {
			if reader.readErr != nil {
				return pickerKeyUnknown, reader.takeReadError()
			}
			if err := reader.readMore(ctx); err != nil && len(reader.pending) == 0 {
				return pickerKeyUnknown, reader.takeReadError()
			}
		}

		parsed := parsePickerKey(reader.pending, reader.readErr != nil)
		if !parsed.incomplete {
			reader.pending = reader.pending[parsed.consumed:]
			return parsed.key, nil
		}

		ready, err := reader.moreInputReady(ctx)
		if err != nil {
			return pickerKeyUnknown, err
		}
		if !ready {
			parsed = parsePickerKey(reader.pending, true)
			reader.pending = reader.pending[parsed.consumed:]
			return parsed.key, nil
		}
		_ = reader.readMore(ctx)
	}
}

func (reader *pickerKeyReader) readMore(ctx context.Context) error {
	if reader.waitBeforeRead {
		for {
			if err := ctx.Err(); err != nil {
				reader.readErr = err
				return err
			}
			ready, err := reader.waitForInput(escapeSequenceWaitTimeout)
			if ctxErr := ctx.Err(); ctxErr != nil {
				reader.readErr = ctxErr
				return ctxErr
			}
			if err != nil {
				reader.readErr = err
				return err
			}
			if ready {
				break
			}
		}
	}
	buffer := make([]byte, 32)
	count, err := reader.reader.Read(buffer)
	reader.pending = append(reader.pending, buffer[:count]...)
	switch {
	case err != nil:
		reader.readErr = err
	case count == 0:
		reader.readErr = io.ErrNoProgress
	}
	return reader.readErr
}

func (reader *pickerKeyReader) takeReadError() error {
	err := reader.readErr
	reader.readErr = nil
	return err
}

func (reader *pickerKeyReader) moreInputReady(ctx context.Context) (bool, error) {
	if reader.readErr != nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if reader.waitForInput == nil {
		return true, nil
	}
	ready, err := reader.waitForInput(escapeSequenceWaitTimeout)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil {
		return false, err
	}
	return ready, nil
}

type parsedPickerKey struct {
	key        pickerKey
	consumed   int
	incomplete bool
}

func parsePickerKey(input []byte, final bool) parsedPickerKey {
	if len(input) == 0 {
		return parsedPickerKey{incomplete: !final}
	}
	switch input[0] {
	case 0, 224:
		return parseWindowsExtendedKey(input, final)
	case 0x1b:
		return parseEscapeSequence(input, final)
	}

	key := pickerKeyUnknown
	switch input[0] {
	case '\r', '\n':
		consumed := 1
		if input[0] == '\r' && len(input) > 1 && input[1] == '\n' {
			consumed++
		}
		return parsedPickerKey{key: pickerKeySelect, consumed: consumed}
	case 3, 'q':
		key = pickerKeyCancel
	case 'k':
		key = pickerKeyUp
	case 'j':
		key = pickerKeyDown
	}
	return parsedPickerKey{key: key, consumed: 1}
}

func parseWindowsExtendedKey(input []byte, final bool) parsedPickerKey {
	if len(input) < 2 {
		if !final {
			return parsedPickerKey{incomplete: true}
		}
		return parsedPickerKey{key: pickerKeyUnknown, consumed: len(input)}
	}
	key := pickerKeyUnknown
	switch input[1] {
	case 72:
		key = pickerKeyUp
	case 80:
		key = pickerKeyDown
	case 73:
		key = pickerKeyPageUp
	case 81:
		key = pickerKeyPageDown
	case 71:
		key = pickerKeyHome
	case 79:
		key = pickerKeyEnd
	}
	return parsedPickerKey{key: key, consumed: 2}
}

func parseEscapeSequence(input []byte, final bool) parsedPickerKey {
	if len(input) == 1 {
		return incompleteEscapeSequence(input, final)
	}
	if input[1] != '[' && input[1] != 'O' {
		return parsedPickerKey{key: pickerKeyCancel, consumed: 1}
	}
	if len(input) == 2 {
		return incompleteEscapeSequence(input, final)
	}

	key := pickerKeyUnknown
	switch input[2] {
	case 'A':
		key = pickerKeyUp
	case 'B':
		key = pickerKeyDown
	case 'H':
		key = pickerKeyHome
	case 'F':
		key = pickerKeyEnd
	case '1', '4', '5', '6', '7', '8':
		if len(input) == 3 {
			return incompleteEscapeSequence(input, final)
		}
		if input[3] != '~' {
			return parsedPickerKey{key: pickerKeyUnknown, consumed: 3}
		}
		switch input[2] {
		case '1', '7':
			key = pickerKeyHome
		case '4', '8':
			key = pickerKeyEnd
		case '5':
			key = pickerKeyPageUp
		case '6':
			key = pickerKeyPageDown
		}
		return parsedPickerKey{key: key, consumed: 4}
	}
	return parsedPickerKey{key: key, consumed: 3}
}

func incompleteEscapeSequence(input []byte, final bool) parsedPickerKey {
	if !final {
		return parsedPickerKey{incomplete: true}
	}
	return parsedPickerKey{key: pickerKeyCancel, consumed: len(input)}
}
