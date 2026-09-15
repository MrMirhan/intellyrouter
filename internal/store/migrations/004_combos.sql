-- A provider's slug names its models without a route: "<slug>/<model_id>".
ALTER TABLE providers ADD COLUMN slug TEXT NOT NULL DEFAULT '';
UPDATE providers SET slug = lower(replace(trim(name), ' ', '-'));

-- A combo is a model of the built-in combo provider. It sends each request to
-- one of its member models, in order or balanced across them, and moves on to
-- the next member when one fails.
CREATE TABLE combos (
  model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
  strategy TEXT NOT NULL
);

CREATE TABLE combo_members (
  combo_id INTEGER NOT NULL REFERENCES combos(model_id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
  PRIMARY KEY (combo_id, position)
);

CREATE INDEX combo_members_model ON combo_members (model_id);
