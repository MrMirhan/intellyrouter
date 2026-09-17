-- A member's share of the combo's traffic. Round-robin gives a member with
-- weight 3 three turns for every one a weight-1 member gets, and least-used
-- compares load per unit of weight. Fallback ignores it: its order is fixed.
ALTER TABLE combo_members ADD COLUMN weight INTEGER NOT NULL DEFAULT 1;
