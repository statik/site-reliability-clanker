-- The role and the database of the application. The Job in database.tf runs this file with
-- psql as the administrator of the server. Each statement can run again: a second run
-- only sets the password again. psql does not print the statements, so the password does
-- not go to the log of the Job.
\set ON_ERROR_STOP on
\set VERBOSITY terse
\getenv app_role APP_ROLE
\getenv app_password APP_PASSWORD

-- The application never gets the administrator, a superuser or a database of another role.
SELECT format('DO $guard$ BEGIN RAISE EXCEPTION USING MESSAGE = %L; END $guard$',
              format('the role %s exists and is the administrator or a superuser; '
                     'an application cannot have this name', rolname))
  FROM pg_roles
 WHERE rolname = :'app_role' AND (rolsuper OR rolname = current_user)
\gexec

SELECT format('DO $guard$ BEGIN RAISE EXCEPTION USING MESSAGE = %L; END $guard$',
              format('the database %s exists and belongs to the role %s, not to %s; '
                     'an application cannot have this name', d.datname, r.rolname,
                     :'app_role'))
  FROM pg_database d
  JOIN pg_roles r ON r.oid = d.datdba
 WHERE d.datname = :'app_role' AND r.rolname <> :'app_role'
\gexec

SELECT format('CREATE ROLE %I LOGIN', :'app_role')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'app_role')
\gexec

SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', :'app_role', :'app_password')
\gexec

-- From PostgreSQL 16, an administrator that is not a superuser (RDS, Cloud SQL, Azure) can
-- give a database to a role only when it can SET ROLE to that role.
SELECT format('GRANT %I TO CURRENT_USER', :'app_role')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'app_role', :'app_role')
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'app_role')
\gexec

SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', :'app_role')
\gexec
