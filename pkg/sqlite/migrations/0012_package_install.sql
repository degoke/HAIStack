-- haistack-sqlite package install completion tracking

CREATE TABLE IF NOT EXISTS hai_package_install (
    package_name    TEXT NOT NULL,
    package_version TEXT NOT NULL,
    completed_at    TEXT NOT NULL,
    PRIMARY KEY (package_name, package_version)
);
