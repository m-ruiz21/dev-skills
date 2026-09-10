package taskloop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rivo/uniseg"
)

type fragmentedReader struct {
	fragments [][]byte
}

func (reader *fragmentedReader) Read(buffer []byte) (int, error) {
	if len(reader.fragments) == 0 {
		return 0, io.EOF
	}
	fragment := reader.fragments[0]
	reader.fragments = reader.fragments[1:]
	return copy(buffer, fragment), nil
}

func TestOpenTerminalPRDSelectorReportsUnavailableOutput(t *testing.T) {
	openErr := errors.New("no controlling terminal")

	selector, available := openTerminalPRDSelector(
		terminalPRDInput{file: os.Stdin},
		func() (*prdPickerTerminalOutput, error) {
			return nil, openErr
		},
	)

	if available || selector != nil {
		t.Fatalf("selector=%v available=%v, want deterministic fallback", selector, available)
	}
}

func TestTerminalPRDSelectorCleansUpOwnedOutputWhenInputCannotBecomeRaw(t *testing.T) {
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer inputWriter.Close()
	outputReader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputReader.Close()
	restoreCalls := 0
	selector := &terminalPRDSelector{
		input: input,
		output: newPRDPickerTerminalOutput(output, func() error {
			restoreCalls++
			return nil
		}),
	}

	_, _, selectErr := selector.Select(context.Background(), []PRDOption{{Path: "prd"}})

	if selectErr == nil || !strings.Contains(selectErr.Error(), "enable keyboard PRD selection") {
		t.Fatalf("error=%v, want raw-terminal setup failure", selectErr)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore calls=%d, want 1", restoreCalls)
	}
	if _, err := output.WriteString("closed"); err == nil {
		t.Fatal("owned terminal output remains open")
	}
	if err := selector.output.Close(); err != nil {
		t.Fatal(err)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore calls after repeated close=%d, want 1", restoreCalls)
	}
}

func TestPRDPickerStateScrollsSelectedItemIntoView(t *testing.T) {
	state := newPRDPickerState(12, 4)
	for range 7 {
		state = state.apply(pickerKeyDown)
	}
	if state.selected != 7 || state.offset != 4 {
		t.Fatalf("after scrolling down: selected=%d offset=%d", state.selected, state.offset)
	}

	state = state.apply(pickerKeyPageDown)
	if state.selected != 11 || state.offset != 8 {
		t.Fatalf("after page down: selected=%d offset=%d", state.selected, state.offset)
	}
	state = state.apply(pickerKeyHome)
	if state.selected != 0 || state.offset != 0 {
		t.Fatalf("after home: selected=%d offset=%d", state.selected, state.offset)
	}
	state = state.apply(pickerKeyUp)
	if state.selected != 0 || state.offset != 0 {
		t.Fatalf("moving above the first item changed state: %+v", state)
	}
}

func TestKeyboardPRDPickerScrollsAndSelectsWithArrowKeys(t *testing.T) {
	options := make([]PRDOption, 12)
	for index := range options {
		options[index] = PRDOption{
			Path:  fmt.Sprintf("path-%02d", index+1),
			Label: fmt.Sprintf(".scratch/feature-%02d/PRD.md", index+1),
		}
	}
	input := strings.NewReader(strings.Repeat("\x1b[B", 7) + "\r")
	var output bytes.Buffer
	picker := keyboardPRDPicker{input: input, output: &output, width: 80, height: 7}

	selected, ok, err := picker.Select(context.Background(), options)
	if err != nil || !ok {
		t.Fatalf("selected=%+v ok=%v err=%v", selected, ok, err)
	}
	if selected.Path != "path-08" {
		t.Fatalf("selected %q, want path-08", selected.Path)
	}
	rendered := output.String()
	if !strings.Contains(rendered, ".scratch/feature-08/PRD.md") ||
		!strings.Contains(rendered, "8/12") ||
		!strings.Contains(rendered, "↑ more") ||
		!strings.Contains(rendered, "↓ more") {
		t.Fatalf("picker did not render the scrolled viewport and position: %q", rendered)
	}
	if !strings.HasSuffix(rendered, "\x1b[?25h") {
		t.Fatalf("picker did not restore the cursor: %q", rendered)
	}
}

func TestKeyboardPRDPickerSupportsPagingAndCancellation(t *testing.T) {
	options := make([]PRDOption, 20)
	for index := range options {
		options[index] = PRDOption{Path: fmt.Sprintf("path-%d", index)}
	}

	t.Run("page down and end", func(t *testing.T) {
		input := strings.NewReader("\x1b[6~\x1b[F\n")
		selected, ok, err := (keyboardPRDPicker{
			input: input, output: &bytes.Buffer{}, width: 40, height: 8,
		}).Select(context.Background(), options)
		if err != nil || !ok || selected.Path != "path-19" {
			t.Fatalf("selected=%+v ok=%v err=%v", selected, ok, err)
		}
	})

	t.Run("escape", func(t *testing.T) {
		selected, ok, err := (keyboardPRDPicker{
			input: strings.NewReader("\x1b"), output: &bytes.Buffer{}, width: 40, height: 8,
		}).Select(context.Background(), options)
		if err != nil || ok || selected != (PRDOption{}) {
			t.Fatalf("selected=%+v ok=%v err=%v", selected, ok, err)
		}
	})
}

func TestKeyboardPRDPickerBuffersFragmentedEscapeSequences(t *testing.T) {
	options := []PRDOption{
		{Path: "first"},
		{Path: "second"},
	}
	tests := []struct {
		name      string
		fragments []string
	}{
		{name: "CSI arrow", fragments: []string{"\x1b", "[", "B", "\r"}},
		{name: "CSI paging", fragments: []string{"\x1b[", "6", "~", "\r"}},
		{name: "Windows extended arrow", fragments: []string{"\xe0", "\x50", "\r"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fragments := make([][]byte, len(test.fragments))
			for index, fragment := range test.fragments {
				fragments[index] = []byte(fragment)
			}
			selected, ok, err := (keyboardPRDPicker{
				input:  &fragmentedReader{fragments: fragments},
				output: &bytes.Buffer{},
				width:  40,
				height: 8,
			}).Select(context.Background(), options)
			if err != nil || !ok || selected.Path != "second" {
				t.Fatalf("selected=%+v ok=%v err=%v", selected, ok, err)
			}
		})
	}
}

func TestPickerKeyReaderWaitsBeforeCancellingIncompleteEscapeSequence(t *testing.T) {
	for _, sequence := range []string{"\x1b", "\x1b["} {
		t.Run(fmt.Sprintf("%q", sequence), func(t *testing.T) {
			waited := false
			reader := pickerKeyReader{
				reader: &fragmentedReader{fragments: [][]byte{[]byte(sequence)}},
				waitForInput: func(timeout time.Duration) (bool, error) {
					waited = timeout == escapeSequenceWaitTimeout
					return false, nil
				},
			}

			key, err := reader.next(context.Background())
			if err != nil || key != pickerKeyCancel || !waited {
				t.Fatalf("key=%v err=%v waited=%v", key, err, waited)
			}
		})
	}
}

func TestKeyboardPRDPickerHandlesEmptyOptions(t *testing.T) {
	selected, ok, err := (keyboardPRDPicker{
		input: strings.NewReader("\n"), output: &bytes.Buffer{}, width: 40, height: 8,
	}).Select(context.Background(), nil)
	if err != nil || ok || selected != (PRDOption{}) {
		t.Fatalf("selected=%+v ok=%v err=%v", selected, ok, err)
	}
}

func TestKeyboardPRDPickerCancellationRestoresCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer

	_, ok, err := (keyboardPRDPicker{
		input: strings.NewReader(""), output: &output, width: 40, height: 8,
	}).Select(ctx, []PRDOption{{Path: "prd"}})

	if ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.HasSuffix(output.String(), "\x1b[?25h") {
		t.Fatalf("picker did not restore the cursor after cancellation: %q", output.String())
	}
}

func TestPickerKeyReaderPrefersCancellationWhenTerminalWaitIsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := pickerKeyReader{
		reader: strings.NewReader(""),
		waitForInput: func(time.Duration) (bool, error) {
			cancel()
			return false, errors.New("terminal wait interrupted")
		},
		waitBeforeRead: true,
	}

	_, err := reader.next(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context cancellation", err)
	}
}

func TestRenderPRDPickerLeavesLastTerminalColumnUnused(t *testing.T) {
	tests := []struct {
		name         string
		width        int
		maxLineWidth int
	}{
		{name: "narrow", width: 12, maxLineWidth: 11},
		{name: "two columns", width: 2, maxLineWidth: 1},
		{name: "one column", width: 1, maxLineWidth: 0},
		{name: "zero columns", width: 0, maxLineWidth: 0},
		{name: "negative width", width: -1, maxLineWidth: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			options := []PRDOption{{Label: "a-very-long-prd-label-that-must-be-truncated"}}
			state := newPRDPickerState(len(options), 1)

			if err := renderPRDPicker(&output, test.width, options, state, false); err != nil {
				t.Fatal(err)
			}

			rendered := strings.TrimSuffix(output.String(), "\n")
			lines := strings.Split(rendered, "\n")
			if len(lines) != 3 {
				t.Fatalf("rendered %d lines, want 3: %q", len(lines), rendered)
			}
			for _, line := range lines {
				line = strings.TrimPrefix(line, "\r\x1b[2K")
				if lineWidth := uniseg.StringWidth(line); lineWidth > test.maxLineWidth {
					t.Errorf("rendered line width %d exceeds safe maximum %d: %q", lineWidth, test.maxLineWidth, line)
				}
			}
		})
	}
}

func TestRenderPRDPickerSanitizesLabelsWithoutChangingSelection(t *testing.T) {
	option := PRDOption{
		Path:  ".scratch/\x1b[31m-danger/PRD.md",
		Label: "safe\x1b[31mred\nnext\t\u009b2J",
	}
	var output bytes.Buffer

	selected, ok, err := (keyboardPRDPicker{
		input: strings.NewReader("\n"), output: &output, width: 80, height: 8,
	}).Select(context.Background(), []PRDOption{option})
	if err != nil || !ok || selected != option {
		t.Fatalf("selected=%+v ok=%v err=%v", selected, ok, err)
	}
	rendered := output.String()
	for _, injected := range []string{"\x1b[31m", "\nnext", "\t", "\u009b2J"} {
		if strings.Contains(rendered, injected) {
			t.Errorf("rendered output contains injected control sequence %q: %q", injected, rendered)
		}
	}
	if !strings.Contains(rendered, "safe�[31mred�next��2J") {
		t.Fatalf("sanitized label is missing from output: %q", rendered)
	}
}

func TestTruncatePRDPickerLineUsesTerminalDisplayCells(t *testing.T) {
	tests := []struct {
		name  string
		value string
		width int
		want  string
	}{
		{name: "CJK", value: "界abc", width: 4, want: "界a…"},
		{name: "emoji", value: "😀abc", width: 4, want: "😀a…"},
		{name: "combining mark", value: "e\u0301abc", width: 3, want: "e\u0301a…"},
		{name: "wide cluster cannot fit prefix", value: "界x", width: 2, want: "…"},
		{name: "single cell", value: "界", width: 1, want: "…"},
		{name: "no cells", value: "界", width: 0, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := truncatePRDPickerLine(test.value, test.width)
			if got != test.want {
				t.Fatalf("truncatePRDPickerLine(%q, %d)=%q, want %q", test.value, test.width, got, test.want)
			}
			if gotWidth := uniseg.StringWidth(got); gotWidth > test.width {
				t.Fatalf("result width %d exceeds %d: %q", gotWidth, test.width, got)
			}
		})
	}
}
