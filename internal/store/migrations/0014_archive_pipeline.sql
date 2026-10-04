ALTER TABLE archive_documents ADD COLUMN document_revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE archive_documents ADD COLUMN active_publication_id TEXT;
ALTER TABLE archive_documents ADD COLUMN original_restricted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE archive_documents ADD COLUMN withdrawn INTEGER NOT NULL DEFAULT 0;
CREATE TABLE archive_products (
 household_id TEXT NOT NULL, task_id TEXT NOT NULL, product_key TEXT NOT NULL, fingerprint TEXT NOT NULL,
 digest TEXT NOT NULL, body BLOB NOT NULL, PRIMARY KEY(household_id,task_id,product_key,fingerprint),
 FOREIGN KEY(household_id,task_id) REFERENCES archive_tasks(household_id,id)
);
CREATE TRIGGER archive_product_immutable BEFORE UPDATE ON archive_products BEGIN SELECT RAISE(ABORT,'immutable archive product'); END;
CREATE TABLE archive_invocations (
 household_id TEXT NOT NULL, task_id TEXT NOT NULL, id TEXT NOT NULL, operation TEXT NOT NULL,
 attempt_id TEXT NOT NULL, reserved INTEGER NOT NULL CHECK(reserved>0), status TEXT NOT NULL CHECK(status IN ('reserved','completed','failed')),
 product_key TEXT NOT NULL DEFAULT '', PRIMARY KEY(household_id,id),
 FOREIGN KEY(household_id,task_id) REFERENCES archive_tasks(household_id,id)
);
CREATE TABLE archive_budgets (
 household_id TEXT NOT NULL, task_id TEXT NOT NULL, maximum INTEGER NOT NULL CHECK(maximum>0), spent INTEGER NOT NULL CHECK(spent>=0 AND spent<=maximum),
 PRIMARY KEY(household_id,task_id), FOREIGN KEY(household_id,task_id) REFERENCES archive_tasks(household_id,id)
);
CREATE TABLE archive_extractions (
 household_id TEXT NOT NULL,id TEXT NOT NULL,task_id TEXT NOT NULL,document_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL, document_revision INTEGER NOT NULL, resolution_revision INTEGER NOT NULL,
 status TEXT NOT NULL DEFAULT 'candidate', PRIMARY KEY(household_id,id), UNIQUE(household_id,task_id,fingerprint,document_revision,resolution_revision),
 FOREIGN KEY(household_id,task_id) REFERENCES archive_tasks(household_id,id),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id)
);
CREATE TABLE archive_subject_events (
 household_id TEXT NOT NULL,id TEXT NOT NULL,document_id TEXT NOT NULL,anchor_key TEXT NOT NULL,
 anchor_rule TEXT NOT NULL,anchors_json TEXT NOT NULL,event_type TEXT NOT NULL,
 PRIMARY KEY(household_id,id), UNIQUE(household_id,document_id,anchor_key),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id)
);
CREATE TABLE archive_observations (
 household_id TEXT NOT NULL,id TEXT NOT NULL,event_id TEXT NOT NULL,slot_key TEXT NOT NULL,
 PRIMARY KEY(household_id,id),UNIQUE(household_id,event_id,slot_key),
 FOREIGN KEY(household_id,event_id) REFERENCES archive_subject_events(household_id,id)
);
CREATE TABLE archive_observation_revisions (
 household_id TEXT NOT NULL,observation_id TEXT NOT NULL,revision INTEGER NOT NULL,
 extraction_id TEXT NOT NULL,body_json TEXT NOT NULL,assertion_kind TEXT NOT NULL DEFAULT 'source',
 PRIMARY KEY(household_id,observation_id,revision),
 FOREIGN KEY(household_id,observation_id) REFERENCES archive_observations(household_id,id),
 FOREIGN KEY(household_id,extraction_id) REFERENCES archive_extractions(household_id,id)
);
CREATE TRIGGER archive_observation_immutable BEFORE UPDATE ON archive_observation_revisions BEGIN SELECT RAISE(ABORT,'immutable observation revision'); END;
CREATE TABLE archive_publications (
 household_id TEXT NOT NULL,id TEXT NOT NULL,document_id TEXT NOT NULL,extraction_id TEXT NOT NULL,
 document_revision INTEGER NOT NULL,classification_json TEXT NOT NULL,status TEXT NOT NULL,coverage_json TEXT NOT NULL,
 PRIMARY KEY(household_id,id),UNIQUE(household_id,document_id,document_revision),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,extraction_id) REFERENCES archive_extractions(household_id,id)
);
CREATE TABLE archive_publication_observations (
 household_id TEXT NOT NULL,publication_id TEXT NOT NULL,observation_id TEXT NOT NULL,revision INTEGER NOT NULL,
 PRIMARY KEY(household_id,publication_id,observation_id),
 FOREIGN KEY(household_id,publication_id) REFERENCES archive_publications(household_id,id),
 FOREIGN KEY(household_id,observation_id,revision) REFERENCES archive_observation_revisions(household_id,observation_id,revision)
);
CREATE TABLE archive_outbox (
 household_id TEXT NOT NULL,id TEXT NOT NULL,kind TEXT NOT NULL,document_id TEXT NOT NULL,revision INTEGER NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',PRIMARY KEY(household_id,id),UNIQUE(household_id,kind,document_id,revision)
);
CREATE TABLE archive_manual_revisions (
 household_id TEXT NOT NULL,id TEXT NOT NULL,document_id TEXT NOT NULL,document_revision INTEGER NOT NULL,
 actor TEXT NOT NULL,kind TEXT NOT NULL,body_json TEXT NOT NULL,created_at TEXT NOT NULL,
 PRIMARY KEY(household_id,id),UNIQUE(household_id,document_id,document_revision),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id)
);
CREATE TRIGGER archive_manual_immutable BEFORE UPDATE ON archive_manual_revisions BEGIN SELECT RAISE(ABORT,'immutable manual revision'); END;
CREATE TABLE archive_private_regions (
 household_id TEXT NOT NULL,document_id TEXT NOT NULL,observation_id TEXT NOT NULL,revision_id TEXT NOT NULL,
 PRIMARY KEY(household_id,document_id,observation_id),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,revision_id) REFERENCES archive_manual_revisions(household_id,id)
);
CREATE TABLE archive_questions (
 household_id TEXT NOT NULL,id TEXT NOT NULL,task_id TEXT NOT NULL,version INTEGER NOT NULL,
 body_json TEXT NOT NULL,answer_json TEXT, resolution_key TEXT,
 PRIMARY KEY(household_id,id), UNIQUE(household_id,task_id,version),
 FOREIGN KEY(household_id,task_id) REFERENCES archive_tasks(household_id,id)
);
CREATE TABLE archive_legacy_outbox (
 record_id TEXT PRIMARY KEY REFERENCES family_records(id),status TEXT NOT NULL DEFAULT 'pending'
);
CREATE TRIGGER archive_legacy_producer AFTER INSERT ON family_records BEGIN
 INSERT INTO archive_legacy_outbox(record_id) VALUES(NEW.id);
END;
CREATE TABLE archive_legacy_projections (
 household_id TEXT NOT NULL,root_record_id TEXT NOT NULL REFERENCES family_records(id),event_id TEXT NOT NULL,
 current_record_id TEXT NOT NULL REFERENCES family_records(id),entity_id TEXT NOT NULL,
 body_json TEXT NOT NULL,active INTEGER NOT NULL,tv_visible INTEGER NOT NULL,
 PRIMARY KEY(household_id,root_record_id),UNIQUE(household_id,event_id),
 FOREIGN KEY(household_id,entity_id) REFERENCES archive_entities(household_id,id)
);
CREATE TABLE archive_legacy_record_events (
 household_id TEXT NOT NULL,record_id TEXT NOT NULL REFERENCES family_records(id),root_record_id TEXT NOT NULL,
 PRIMARY KEY(household_id,record_id),
 FOREIGN KEY(household_id,root_record_id) REFERENCES archive_legacy_projections(household_id,root_record_id)
);
CREATE TABLE archive_transaction_events (
 household_id TEXT NOT NULL,transaction_type TEXT NOT NULL,issuer TEXT NOT NULL,identifier TEXT NOT NULL,event_id TEXT NOT NULL,
 PRIMARY KEY(household_id,transaction_type,issuer,identifier),
 FOREIGN KEY(household_id,event_id) REFERENCES archive_subject_events(household_id,id)
);
CREATE TABLE archive_event_equivalences (
 household_id TEXT NOT NULL,event_id TEXT NOT NULL,canonical_event_id TEXT NOT NULL,proof_json TEXT NOT NULL,
 PRIMARY KEY(household_id,event_id),FOREIGN KEY(household_id,event_id) REFERENCES archive_subject_events(household_id,id),
 FOREIGN KEY(household_id,canonical_event_id) REFERENCES archive_subject_events(household_id,id)
);
CREATE TABLE archive_observation_alternatives (
 household_id TEXT NOT NULL,observation_id TEXT NOT NULL,extraction_id TEXT NOT NULL,body_json TEXT NOT NULL,
 PRIMARY KEY(household_id,observation_id,extraction_id),
 FOREIGN KEY(household_id,observation_id) REFERENCES archive_observations(household_id,id),
 FOREIGN KEY(household_id,extraction_id) REFERENCES archive_extractions(household_id,id)
);
CREATE TABLE archive_extraction_products (
 household_id TEXT NOT NULL,extraction_id TEXT NOT NULL,task_id TEXT NOT NULL,product_key TEXT NOT NULL,fingerprint TEXT NOT NULL,digest TEXT NOT NULL,
 PRIMARY KEY(household_id,extraction_id,product_key),
 FOREIGN KEY(household_id,extraction_id) REFERENCES archive_extractions(household_id,id),
 FOREIGN KEY(household_id,task_id,product_key,fingerprint) REFERENCES archive_products(household_id,task_id,product_key,fingerprint)
);
CREATE TRIGGER archive_extraction_product_immutable BEFORE UPDATE ON archive_extraction_products BEGIN SELECT RAISE(ABORT,'immutable extraction product'); END;

CREATE TABLE archive_publication_alternatives (
 household_id TEXT NOT NULL,publication_id TEXT NOT NULL,observation_id TEXT NOT NULL,extraction_id TEXT NOT NULL,
 PRIMARY KEY(household_id,publication_id,observation_id),
 FOREIGN KEY(household_id,publication_id) REFERENCES archive_publications(household_id,id),
 FOREIGN KEY(household_id,observation_id,extraction_id) REFERENCES archive_observation_alternatives(household_id,observation_id,extraction_id)
);
