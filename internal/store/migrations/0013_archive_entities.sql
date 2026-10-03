-- Identity objects never create account/member rows or change integration policy.
CREATE TABLE archive_resolution_revisions (
 household_id TEXT PRIMARY KEY REFERENCES households(id),
 revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0)
);
INSERT INTO archive_resolution_revisions(household_id) SELECT id FROM households;
CREATE TRIGGER archive_resolution_household AFTER INSERT ON households BEGIN
 INSERT INTO archive_resolution_revisions(household_id) VALUES(NEW.id);
END;
CREATE TABLE archive_entities (
 household_id TEXT NOT NULL REFERENCES households(id), id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('person','property','device','household')),
 version INTEGER NOT NULL CHECK(version>0), PRIMARY KEY(household_id,id)
);
CREATE TABLE archive_entity_attributes (
 household_id TEXT NOT NULL, id TEXT NOT NULL, entity_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('name','display_alias','birth','full_address')),
 value TEXT NOT NULL, precision TEXT NOT NULL DEFAULT '',
 document_id TEXT, legacy_member_id TEXT REFERENCES family_members(id), link_id TEXT,
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 tv_visible INTEGER NOT NULL CHECK(tv_visible IN (0,1)),
 conflict INTEGER NOT NULL DEFAULT 0 CHECK(conflict IN (0,1)),
 PRIMARY KEY(household_id,id),
 FOREIGN KEY(household_id,entity_id) REFERENCES archive_entities(household_id,id),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,link_id) REFERENCES archive_entity_links(household_id,id),
 CHECK((document_id IS NOT NULL AND legacy_member_id IS NULL AND link_id IS NOT NULL) OR (document_id IS NULL AND legacy_member_id IS NOT NULL AND link_id IS NULL))
);
CREATE TABLE archive_entity_bindings (
 household_id TEXT NOT NULL, id TEXT NOT NULL, scope TEXT NOT NULL CHECK(scope IN ('legacy_member','document_mention')),
 scope_key TEXT NOT NULL, entity_id TEXT NOT NULL, confirmed_by TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0), evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 conflict INTEGER NOT NULL DEFAULT 0 CHECK(conflict IN (0,1)),
 PRIMARY KEY(household_id,id), UNIQUE(household_id,scope,scope_key),
 FOREIGN KEY(household_id,entity_id) REFERENCES archive_entities(household_id,id)
);
CREATE TABLE archive_entity_merges (
 household_id TEXT NOT NULL, id TEXT NOT NULL, source_id TEXT NOT NULL, target_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0), active INTEGER NOT NULL CHECK(active IN (0,1)),
 actor TEXT NOT NULL, reason TEXT NOT NULL, created_revision INTEGER NOT NULL,
 PRIMARY KEY(household_id,id), CHECK(source_id<>target_id),
 FOREIGN KEY(household_id,source_id) REFERENCES archive_entities(household_id,id),
 FOREIGN KEY(household_id,target_id) REFERENCES archive_entities(household_id,id)
);
CREATE TABLE archive_entity_redirects (
 household_id TEXT NOT NULL, source_id TEXT NOT NULL, target_id TEXT NOT NULL, merge_id TEXT NOT NULL,
 PRIMARY KEY(household_id,source_id), CHECK(source_id<>target_id),
 FOREIGN KEY(household_id,source_id) REFERENCES archive_entities(household_id,id),
 FOREIGN KEY(household_id,target_id) REFERENCES archive_entities(household_id,id),
 FOREIGN KEY(household_id,merge_id) REFERENCES archive_entity_merges(household_id,id)
);
CREATE TABLE archive_entity_links (
 household_id TEXT NOT NULL, id TEXT NOT NULL, document_id TEXT NOT NULL, mention_ref TEXT NOT NULL,
 role TEXT NOT NULL, matched_entity_id TEXT, status TEXT NOT NULL CHECK(status IN ('linked','needs_resolution')),
 version INTEGER NOT NULL CHECK(version>0), created_revision INTEGER NOT NULL,
 PRIMARY KEY(household_id,id), UNIQUE(household_id,document_id,mention_ref),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,matched_entity_id) REFERENCES archive_entities(household_id,id)
);
CREATE TABLE archive_entity_link_merge_context (
 household_id TEXT NOT NULL, link_id TEXT NOT NULL, merge_id TEXT NOT NULL,
 PRIMARY KEY(household_id,link_id,merge_id),
 FOREIGN KEY(household_id,link_id) REFERENCES archive_entity_links(household_id,id),
 FOREIGN KEY(household_id,merge_id) REFERENCES archive_entity_merges(household_id,id)
);
CREATE TABLE archive_entity_history (
 household_id TEXT NOT NULL, id TEXT NOT NULL, object_id TEXT NOT NULL, action TEXT NOT NULL,
 actor TEXT NOT NULL, reason TEXT NOT NULL, before_json TEXT NOT NULL CHECK(json_valid(before_json)),
 after_json TEXT NOT NULL CHECK(json_valid(after_json)), PRIMARY KEY(household_id,id),
 FOREIGN KEY(household_id) REFERENCES households(id)
);
CREATE TABLE archive_legacy_entities (
 household_id TEXT NOT NULL, legacy_member_id TEXT NOT NULL REFERENCES family_members(id), entity_id TEXT NOT NULL,
 PRIMARY KEY(household_id,legacy_member_id),
 FOREIGN KEY(household_id,entity_id) REFERENCES archive_entities(household_id,id)
);
-- Raw identity values are protected facts only. No value/hash index, FTS,
-- candidate DTO, logger, navigation, or public projection uses this table.
CREATE TABLE archive_protected_identity_facts (
 household_id TEXT NOT NULL, id TEXT NOT NULL, entity_id TEXT NOT NULL, document_id TEXT NOT NULL,
 kind TEXT NOT NULL, issuer TEXT NOT NULL, rule_version TEXT NOT NULL, value TEXT NOT NULL, link_id TEXT NOT NULL,
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)), conflict INTEGER NOT NULL CHECK(conflict IN (0,1)),
 PRIMARY KEY(household_id,id),
 FOREIGN KEY(household_id,entity_id) REFERENCES archive_entities(household_id,id),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,link_id) REFERENCES archive_entity_links(household_id,id)
);
CREATE INDEX archive_protected_identity_partition ON archive_protected_identity_facts(household_id,kind,issuer);
CREATE TRIGGER archive_entities_revision_insert AFTER INSERT ON archive_entities BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entities_revision_update AFTER UPDATE ON archive_entities BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entities_revision_delete AFTER DELETE ON archive_entities BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_entity_attributes_revision_insert AFTER INSERT ON archive_entity_attributes BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_attributes_revision_update AFTER UPDATE ON archive_entity_attributes BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_attributes_revision_delete AFTER DELETE ON archive_entity_attributes BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_entity_bindings_revision_insert AFTER INSERT ON archive_entity_bindings BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_bindings_revision_update AFTER UPDATE ON archive_entity_bindings BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_bindings_revision_delete AFTER DELETE ON archive_entity_bindings BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_entity_merges_revision_insert AFTER INSERT ON archive_entity_merges BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_merges_revision_update AFTER UPDATE ON archive_entity_merges BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_merges_revision_delete AFTER DELETE ON archive_entity_merges BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_entity_redirects_revision_insert AFTER INSERT ON archive_entity_redirects BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_redirects_revision_update AFTER UPDATE ON archive_entity_redirects BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_redirects_revision_delete AFTER DELETE ON archive_entity_redirects BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_legacy_entities_revision_insert AFTER INSERT ON archive_legacy_entities BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_legacy_entities_revision_update AFTER UPDATE ON archive_legacy_entities BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_legacy_entities_revision_delete AFTER DELETE ON archive_legacy_entities BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_protected_identity_facts_revision_insert AFTER INSERT ON archive_protected_identity_facts BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_protected_identity_facts_revision_update AFTER UPDATE ON archive_protected_identity_facts BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_protected_identity_facts_revision_delete AFTER DELETE ON archive_protected_identity_facts BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
CREATE TRIGGER archive_entity_links_revision_insert AFTER INSERT ON archive_entity_links BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_links_revision_update AFTER UPDATE ON archive_entity_links BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=NEW.household_id;
END;
CREATE TRIGGER archive_entity_links_revision_delete AFTER DELETE ON archive_entity_links BEGIN
 UPDATE archive_resolution_revisions SET revision=revision+1 WHERE household_id=OLD.household_id;
END;
