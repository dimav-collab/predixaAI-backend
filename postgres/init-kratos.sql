-- Creates the kratos database on first postgres startup.
SELECT 'CREATE DATABASE kratos'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'kratos')\gexec
