CREATE TABLE archive_search_state (
 household_id TEXT PRIMARY KEY REFERENCES households(id), epoch INTEGER NOT NULL DEFAULT 1,
 generation INTEGER NOT NULL DEFAULT 1, index_revision INTEGER NOT NULL DEFAULT 1
);
INSERT INTO archive_search_state(household_id) SELECT id FROM households;
CREATE TRIGGER archive_search_household AFTER INSERT ON households BEGIN INSERT INTO archive_search_state(household_id) VALUES(NEW.id); END;
CREATE TABLE archive_search_rows (
 id INTEGER PRIMARY KEY, household_id TEXT NOT NULL, document_id TEXT NOT NULL,
 public INTEGER NOT NULL, epoch INTEGER NOT NULL, generation INTEGER NOT NULL,
 body TEXT NOT NULL, text TEXT NOT NULL,
 UNIQUE(household_id,document_id,public),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id)
);
CREATE VIRTUAL TABLE archive_search_fts USING fts5(text, content='archive_search_rows', content_rowid='id', tokenize='unicode61');
CREATE TRIGGER archive_search_insert AFTER INSERT ON archive_search_rows BEGIN INSERT INTO archive_search_fts(rowid,text) VALUES(NEW.id,NEW.text); END;
CREATE TRIGGER archive_search_delete AFTER DELETE ON archive_search_rows BEGIN INSERT INTO archive_search_fts(archive_search_fts,rowid,text) VALUES('delete',OLD.id,OLD.text); END;
CREATE TRIGGER archive_search_update AFTER UPDATE ON archive_search_rows BEGIN INSERT INTO archive_search_fts(archive_search_fts,rowid,text) VALUES('delete',OLD.id,OLD.text); INSERT INTO archive_search_fts(rowid,text) VALUES(NEW.id,NEW.text); END;
CREATE TRIGGER search_epoch_archive_documents_insert AFTER INSERT ON archive_documents BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_documents_update AFTER UPDATE ON archive_documents BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_documents_delete AFTER DELETE ON archive_documents BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=OLD.household_id; END;
CREATE TRIGGER search_epoch_archive_resolution_revisions_insert AFTER INSERT ON archive_resolution_revisions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_resolution_revisions_update AFTER UPDATE ON archive_resolution_revisions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_resolution_revisions_delete AFTER DELETE ON archive_resolution_revisions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=OLD.household_id; END;
CREATE TRIGGER search_epoch_archive_private_regions_insert AFTER INSERT ON archive_private_regions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_private_regions_update AFTER UPDATE ON archive_private_regions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_private_regions_delete AFTER DELETE ON archive_private_regions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=OLD.household_id; END;
CREATE TRIGGER search_epoch_archive_manual_revisions_insert AFTER INSERT ON archive_manual_revisions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_manual_revisions_update AFTER UPDATE ON archive_manual_revisions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_manual_revisions_delete AFTER DELETE ON archive_manual_revisions BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=OLD.household_id; END;
CREATE TRIGGER search_epoch_archive_legacy_projections_insert AFTER INSERT ON archive_legacy_projections BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_legacy_projections_update AFTER UPDATE ON archive_legacy_projections BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_archive_legacy_projections_delete AFTER DELETE ON archive_legacy_projections BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=OLD.household_id; END;

CREATE TRIGGER search_epoch_legacy_insert AFTER INSERT ON family_records BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=(SELECT household_id FROM local_household WHERE singleton=1); END;

CREATE TRIGGER search_epoch_source AFTER INSERT ON archive_sources BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
CREATE TRIGGER search_epoch_task_status AFTER UPDATE OF status ON archive_tasks WHEN NEW.status IS NOT OLD.status BEGIN UPDATE archive_search_state SET epoch=epoch+1 WHERE household_id=NEW.household_id; END;
