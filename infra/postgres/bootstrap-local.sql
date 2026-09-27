INSERT INTO users (email,password_hash,status)
SELECT 'local@nexora.dev','local-development-only','active'
WHERE NOT EXISTS (SELECT 1 FROM users);

INSERT INTO projects (user_id,name,status)
SELECT id,'Local Project','active' FROM users
WHERE email='local@nexora.dev'
  AND NOT EXISTS (SELECT 1 FROM projects WHERE name='Local Project');
