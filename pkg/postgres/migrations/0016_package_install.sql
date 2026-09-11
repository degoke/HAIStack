-- haistack-postgres package install completion tracking

CREATE TABLE IF NOT EXISTS hai_package_install (
    package_name    TEXT NOT NULL,
    package_version TEXT NOT NULL,
    completed_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (package_name, package_version)
);

INSERT INTO hai_package_install (package_name, package_version, completed_at)
SELECT package_name, package_version, MAX(installed_at)
FROM hai_definition_resource
WHERE package_name != '' AND package_version != ''
GROUP BY package_name, package_version
ON CONFLICT (package_name, package_version) DO NOTHING;
