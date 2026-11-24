package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// s.toJSONSchema() is what s.parse() takes as a JSON Schema, draft 2020-12 (docs/KITWORK_SCHEMA.md mục 8, slice 7):
// each field's type (with null when .nullable()), its rules in JSON Schema's words, its default, .label() as title
// and .description(); required the fields parse needs. A column the engine fills and a .secret() column are not in
// it; the object is not closed, since parse leaves a key it does not have out.
//
// CONTROL (07/11/2026): with jsonSchema not leaving out generated and secret columns, id, passwordHash and createdAt
// were in properties and id in required.
func TestAStructIsAJSONSchema(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/core/schema/shop.work": `import { struct, id, string, integer, decimal, boolean, date, choice, uuid, array, json, timestamp, foreign, float } from "kitwork/schema";
import { connection } from "kitwork/database";

const address = struct({ city: string().min(1), zip: string().nullable() });

export const users = struct({
  id: id(),
  email: string().max(254).email().unique().label("Email").description("Dùng để đăng nhập"),
  name: string().min(1).max(100).label("Họ tên"),
  age: integer().min(0).nullable(),
  plan: choice("free", "pro").default("free"),
  tier: choice("a", "b").nullable(),
  amount: decimal(2).min(0).max(1000000),
  ratio: float(),
  agree: boolean().default(false),
  day: date().nullable(),
  ref: uuid().nullable(),
  meta: json().nullable(),
  shipping: json(address).nullable(),
  passwordHash: string().secret(),
  createdAt: timestamp().created()
});
export const orders = struct({ id: id("uuid"), userId: foreign(users.id), at: timestamp() });
export const contact = struct({ address, note: string().max(2000), tags: array(string().min(1)).max(3).nullable() });

export const db = connection().tables({ users, orders });
`,
		"apps/shop/shop.test/router.work": routerImport + `import { users, orders, contact } from "app/core/schema/shop.work";
router.get((context) => context.json({ users: users.toJSONSchema(), orders: orders.toJSONSchema(), contact: contact.toJSONSchema(),
  argument: safe(() => users.toJSONSchema(1)).error?.message }));
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	read := answer(t, app, "/")
	expect(t, read, map[string]string{
		"users": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"email": {"type": "string", "title": "Email", "description": "Dùng để đăng nhập", "maxLength": 254, "format": "email"},
				"name": {"type": "string", "title": "Họ tên", "minLength": 1, "maxLength": 100},
				"age": {"type": ["integer", "null"], "minimum": 0},
				"plan": {"type": "string", "enum": ["free", "pro"], "default": "free"},
				"tier": {"type": ["string", "null"], "enum": ["a", "b", null]},
				"amount": {"type": "number", "minimum": 0, "maximum": 1000000},
				"ratio": {"type": "number"},
				"agree": {"type": "boolean", "default": false},
				"day": {"type": ["string", "null"], "format": "date"},
				"ref": {"type": ["string", "null"], "format": "uuid"},
				"meta": {},
				"shipping": {"type": ["object", "null"], "properties": {"city": {"type": "string", "minLength": 1}, "zip": {"type": ["string", "null"]}}, "required": ["city"]}
			},
			"required": ["email", "name", "amount", "ratio"]
		}`,
		"orders": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {"userId": {"type": "integer"}, "at": {"type": "integer"}},
			"required": ["userId", "at"]
		}`,
		"contact": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"address": {"type": "object", "properties": {"city": {"type": "string", "minLength": 1}, "zip": {"type": ["string", "null"]}}, "required": ["city"]},
				"note": {"type": "string", "maxLength": 2000},
				"tags": {"type": ["array", "null"], "items": {"type": "string", "minLength": 1}, "maxItems": 3}
			},
			"required": ["address", "note"]
		}`,
	})
	if message, _ := read["argument"].(string); !strings.HasSuffix(message, "toJSONSchema() không nhận đối số") {
		t.Errorf("toJSONSchema(1) = %q", message)
	}
}

// .label() and .description() take one text written; toJSONSchema is a struct's method, no field's name.
func TestALabelWrittenWrongStopsTheBoot(t *testing.T) {
	for _, c := range []struct{ field, says string }{
		{`name: string().label("")`, `.label nhận một chữ viết thẳng: .label("Họ tên")`},
		{`name: string().description(1)`, `.description nhận một chữ viết thẳng: .description("Họ tên")`},
		{`toJSONSchema: string()`, `trường toJSONSchema trùng tên phương thức của struct (parse, pick, form, toJSONSchema)`},
	} {
		said := bootError(t, map[string]string{
			"application.work": manifest,
			"apps/shop/core/schema/shop.work": `import { struct, string } from "kitwork/schema";
export const contact = struct({ ` + c.field + ` });
`,
			"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("ok"));
`,
		})
		if !strings.Contains(said, c.says) {
			t.Errorf("%s: the boot says %q, want %q", c.field, said, c.says)
		}
	}
}
