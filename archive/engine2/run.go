// Package engine2 is Kitwork: it reads application.work, opens what the file declares and serves it. The
// host that does so is host/; the language is core/; each "kitwork/<word>" is library/<word>.
package engine2

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/library/application"
	"github.com/kitwork/engine2/library/database"
)

// Run reads the file, boots and serves. It returns only when serving ends or a step fails.
func Run(file string) error {
	h, err := host.Read(file)
	if err != nil {
		return err
	}
	if err := h.Boot(); err != nil {
		return err
	}
	return untilStopped(h, h.Serve)
}

// RunDatabase is kitwork database: it reads the file and serves app.database's port alone, no site booted, no app's
// code run (Host.ServeDatabase). It returns only when the port stops or a step fails.
func RunDatabase(file string) error {
	h, err := host.Read(file)
	if err != nil {
		return err
	}
	return untilStopped(h, h.ServeDatabase)
}

// untilStopped runs serve, and stops h gracefully at the first Ctrl+C or SIGTERM (Host.Stop, host/shutdown.go): the
// requests going on end, the files close. A second one ends the process at once. On Windows, closing the console,
// logging off and shutting down arrive as SIGTERM.
func untilStopped(h *host.Host, serve func() error) error {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(signals)
		close(signals)
	}()
	go func() {
		if _, ok := <-signals; !ok {
			return
		}
		h.Stop()
		if _, ok := <-signals; ok {
			fmt.Fprintln(os.Stderr, "kitwork: stopped at once, the files not closed")
			os.Exit(1)
		}
	}()
	return serve()
}

// Command is kitwork <name> [words…] for a name that is no word of kitwork itself: it reads the file, boots no site, and
// runs the command the manifest declares by that name with the words typed after it (docs/KITWORK_COMMANDS.md mục 3),
// its context.print to out, its console and every failure to errOut. A word no command answers is said with the words
// there are, never a server started. The first Ctrl+C or SIGTERM cancels the command; a second ends the process at
// once. It returns the exit code: 0 done, 1 the command failed, 2 no such command or words it cannot read.
func Command(file, name string, words []string, out, errOut io.Writer) int {
	h, err := host.ReadWithLog(file, io.Discard)
	if err != nil {
		fmt.Fprintf(errOut, "kitwork: %s is no word of kitwork (%s), and %s, which declares the others, does not read: %v\n", name, strings.Join(application.EngineCommands, ", "), file, err)
		return 2
	}
	if h.Manifest.Commands[name] == nil {
		declared := "declares none (app.commands({ … }))"
		if names := h.CommandNames(); len(names) > 0 {
			declared = "declares " + strings.Join(names, ", ")
		}
		fmt.Fprintf(errOut, "kitwork: %s is no command: kitwork has %s; %s %s\n", name, strings.Join(application.EngineCommands, ", "), file, declared)
		return 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(signals)
		close(signals)
	}()
	go func() {
		if _, ok := <-signals; !ok {
			return
		}
		cancel()
		if _, ok := <-signals; ok {
			fmt.Fprintf(errOut, "kitwork %s: stopped at once\n", name)
			os.Exit(1)
		}
	}()
	if err := h.RunCommand(ctx, name, words, out, errOut); err != nil {
		fmt.Fprintf(errOut, "kitwork %s: %v\n", name, err)
		if errors.Is(err, host.ErrCommandWords) {
			return 2
		}
		return 1
	}
	return 0
}

// CommandNames are the names of the commands file declares, sorted, for kitwork help.
func CommandNames(file string) ([]string, error) {
	h, err := host.ReadWithLog(file, io.Discard)
	if err != nil {
		return nil, err
	}
	return h.CommandNames(), nil
}

// Check reads and boots without serving: everything Run would reject, Check rejects, and every app Run would
// close and serve as 503, Check names.
func Check(file string) error {
	h, err := host.Read(file)
	if err != nil {
		return err
	}
	if err := h.Boot(); err != nil {
		return err
	}
	return h.Failed()
}

// CheckMap is Check with kitwork check's map (host Map; slice 2c of root docs/KITWORK_MODULE_WORK.md, 17/11/2026): for
// each app the boot read, what its module.work declares, its sites, their pages, its tables, jobs, schedules and rules,
// each with the place that declares it. The map is given whether an app closed or not, the closed ones saying why; the
// error is Check's.
func CheckMap(file string) (string, error) {
	h, err := host.Read(file)
	if err != nil {
		return "", err
	}
	if err := h.Boot(); err != nil {
		return "", err
	}
	return h.Map(), h.Failed()
}

// Migrate is kitwork migrate on engine2 (docs/schema.md mục 8, slice 2): it says what each database of each app lacks
// of its .shared/schema/, the statements it would run, asks, then adds it; yes answers for the runner. Nothing a file
// has is changed. It returns the exit code: 0, or 1 when a step failed.
func Migrate(file string, yes bool, input io.Reader, output io.Writer) int {
	h, err := host.ReadWithLog(file, io.Discard)
	if err != nil {
		fmt.Fprintln(output, "kitwork migrate:", err)
		return 1
	}
	plans, err := h.Migrations()
	if err != nil {
		fmt.Fprintln(output, "kitwork migrate:", err)
		return 1
	}
	if len(plans) == 0 {
		fmt.Fprintln(output, "kitwork migrate: không app nào khai database (.shared/schema/)")
		return 0
	}
	say := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintln(output, "  "+title)
		for _, line := range lines {
			fmt.Fprintln(output, "    "+line)
		}
	}
	reader := bufio.NewReader(input)
	ask := func(prompt string) (string, bool) {
		fmt.Fprint(output, prompt)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(output)
			return "", false
		}
		return strings.TrimSpace(line), true
	}
	var pending []*database.Plan
	left := false
	for _, plan := range plans {
		where := plan.Database + " (app " + plan.App + ")"
		if !plan.Pending() && len(plan.Differ) == 0 && plan.Question() == nil {
			fmt.Fprintf(output, "kitwork migrate: %s có mọi thứ code khai\n", where)
			continue
		}
		fmt.Fprintf(output, "kitwork migrate: %s\n", where)
		// what the declaration cannot tell, a rename, a drop, a number's new scale: asked, numbered; with --yes
		// nothing is answered for the runner, and the database is left as it is
		asked := true
		for question := plan.Question(); question != nil; question = plan.Question() {
			if yes {
				fmt.Fprintf(output, "  cần trả lời (chạy kitwork migrate không --yes): %s\n", question.Text)
				asked = false
				break
			}
			fmt.Fprintln(output, "  "+question.Text)
			for index, option := range question.Options {
				fmt.Fprintf(output, "    %d) %s\n", index+1, option)
			}
			answered := false
			for range 3 {
				line, ok := ask(fmt.Sprintf("  Chọn [1-%d]: ", len(question.Options)))
				if !ok {
					break
				}
				if choice, err := strconv.Atoi(line); err == nil && question.Answer(choice-1) == nil {
					answered = true
					break
				}
			}
			if !answered {
				fmt.Fprintln(output, "  không trả lời: database này giữ nguyên")
				asked = false
				break
			}
		}
		if !asked {
			left = true
			continue
		}
		say("database chưa có, kitwork migrate thêm:", plan.Add)
		say("kitwork migrate ghi thêm (câu vẫn chạy khi chưa có):", plan.Write)
		say("khác database, kitwork migrate không đổi (sửa struct theo database):", plan.Differ)
		if len(plan.Statements) > 0 {
			shown := make([]string, len(plan.Statements))
			for i, statement := range plan.Statements {
				shown[i] = strings.ReplaceAll(statement.Shown(), "\n", "\n    ")
			}
			say("câu sẽ chạy:", shown)
		}
		if plan.Pending() {
			pending = append(pending, plan)
		}
	}
	code := 0
	if left {
		code = 1 // a database still waits for an answer
	}
	if len(pending) == 0 {
		fmt.Fprintln(output, "kitwork migrate: không đổi gì")
		return code
	}
	if !yes {
		answer, _ := ask("Thêm vào database? [y/N] ")
		if answer = strings.ToLower(answer); answer != "y" && answer != "yes" {
			fmt.Fprintln(output, "kitwork migrate: không đổi gì")
			return code
		}
	}
	for _, plan := range pending {
		if err := plan.Apply(context.Background()); err != nil {
			fmt.Fprintf(output, "kitwork migrate: %v\n", err)
			code = 1
			continue
		}
		if plan.Sent {
			fmt.Fprintf(output, "kitwork migrate: %s xong, qua %s\n", plan.Database, plan.Through)
			say("cổng không có câu để ghi, chạy lại kitwork migrate khi server dừng:", plan.Left)
			continue
		}
		fmt.Fprintf(output, "kitwork migrate: %s xong\n", plan.Database)
	}
	return code
}
