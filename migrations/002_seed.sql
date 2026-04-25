-- Seed admin user (password: admin123)
INSERT OR IGNORE INTO users (id, username, password, display_name, role, enabled)
VALUES (
    'admin-00000000-0000-0000-0000-000000000001',
    'admin',
    '$2a$10$bm5z8olDftDtwTyevhPxlerg9dgzPeCn1objrz4ZhYbiNep8K1E2O',
    'Administrator',
    'admin',
    1
);
