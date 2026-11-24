package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/host"
)

// Mốc nghiệm thu (23/11/2026): application.work của docs/host/blueprint, bản chép ở testdata/blueprint (chỉ đổi những gì
// đã chốt, xem đầu file), boot trên engine3 với host/system.work và host/maintenance.work giả lập. File dùng giải cấu
// trúc, spread, for…of, import file, và năm cửa. host/system.work đọc KitDB (lát 3): bản chép có các file database của nó.
func TestTheBlueprintOfDocsHostBoots(t *testing.T) {
	folder := blueprintFolder(t)
	source, err := os.ReadFile(filepath.Join(folder, "application.work"))
	if err != nil {
		t.Fatal(err)
	}
	booted, err := host.Program{File: "application.work", Source: string(source), Folder: folder,
		System: func(string) (string, bool) { return "", false }}.Boot()
	if err != nil {
		t.Fatal(err)
	}
	defer booted.Close()
	if len(booted.Notes) != 0 {
		t.Errorf("notes: %q", booted.Notes)
	}
	a, _ := value.As[application.Application](booted.Blueprint)
	f := a.Fields()
	if !a.Running() || a.Checking() || f.Memory.String() != "2gb" || f.Timezone.String() != "Asia/Ho_Chi_Minh" {
		t.Errorf("application: running %v, memory %v, timezone %v", a.Running(), f.Memory, f.Timezone)
	}

	w, ok := value.As[website.Website](f.Website)
	if !ok {
		t.Fatal("no website")
	}
	wf := w.Fields()
	if len(wf.Listen) != 2 || wf.Listen[0].Number() != 443 || wf.Listen[1].Number() != 80 || wf.Certificates.String() != ".certificates" ||
		wf.Concurrency.Number() != 16 || !wf.Warm.IsFunction() || !wf.SSL.IsFunction() || len(wf.Guards) != 5 ||
		!wf.Routes.IsFunction() || !wf.Memory.IsFunction() || !wf.Energy.IsFunction() || len(wf.Errors) != 1 || len(wf.Finally) != 1 {
		t.Errorf("website: %d listen, %d guards", len(wf.Listen), len(wf.Guards))
	}

	d, ok := value.As[database.Database](f.Database)
	if !ok || d.Fields().Open.String() != ".database" || len(d.Fields().Listen) != 1 || d.Fields().Listen[0].Number() != 5445 ||
		d.Fields().Password.String() != "secret" {
		t.Error("database: .database, port 5445, the password from .env")
	}

	s, ok := value.As[schedule.Schedule](f.Schedule)
	if !ok || !s.Fields().Folders.IsFunction() || !s.Fields().Memory.IsFunction() || len(s.Fields().Errors) != 1 {
		t.Fatal("schedule: folders, memory, error")
	}
	c, ok := value.As[command.Command](f.Command)
	if !ok || c.Fields().Name.String() != "kitcli" || len(c.Fields().Handle) != 1 || len(c.Fields().Errors) != 1 {
		t.Fatal("command: kitcli, one handle, one error")
	}
	handle := c.Fields().Handle[0]
	if keys := strings.Join(handle.Keys(), ","); keys != "backup,migrate,tenants" {
		t.Errorf("the commands: %s", keys)
	}

	booted.Blueprint.FreezeShared()

	// các hàm của bản thiết kế chạy được trên VM, như engine sẽ gọi chúng: các thư mục có lịch (spread của một map)
	m := runtime.New(runtime.Options{})
	folders := m.Call(s.Fields().Folders)
	if folders.IsInvalid() || strings.Join(itemTexts(folders), " ") != ".schedules apps/03dt/.schedules" {
		t.Errorf("schedule folders: %v %v", itemTexts(folders), folders.Error())
	}
	memory := m.Call(s.Fields().Memory, value.Object().Set("job", value.Object().Set("folder", value.String(".schedules"))))
	if memory.String() != "1gb" {
		t.Errorf("the memory of a host job: %v", memory)
	}
	// các lệnh: for…of trên các tenant đang chạy, và các hàm import từ host/maintenance.work
	context := value.Object().Set("options", value.Object().Set("yes", value.Boolean(true)))
	for _, name := range []string{"tenants", "backup", "migrate"} {
		f, _ := handle.Get(name)
		if r := m.Call(f, context); r.IsInvalid() {
			t.Errorf("kitcli %s: %v", name, r.Error())
		}
	}
	// routes đọc tenant mà guard đặt, bằng giải cấu trúc
	request := value.Object().Set("tenant", value.Object().Set("identity", value.String("03dt"))).Set("hostname", value.String("shop.vn"))
	if r := m.Call(wf.Routes, value.Object().Set("request", request)); r.String() != "apps/03dt/shop.vn" {
		t.Errorf("routes: %v %v", r, r.Error())
	}
}

// itemTexts là các chuỗi của một mảng.
func itemTexts(v value.Value) []string {
	var out []string
	for _, item := range v.Items() {
		out = append(out, item.String())
	}
	return out
}
