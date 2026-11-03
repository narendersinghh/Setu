<?php
return [
// Emails tab: threading, sender/recipient address and unread state
"ALTER TABLE messages ADD msg_id VARCHAR(250) NOT NULL DEFAULT '',ADD in_reply_to VARCHAR(250) NOT NULL DEFAULT '',ADD addr VARCHAR(190) NOT NULL DEFAULT '',ADD is_read TINYINT(1) NOT NULL DEFAULT 1,ADD KEY msgid (msg_id),ADD KEY unread (channel,direction,is_read)",
"UPDATE messages SET msg_id=ext_id WHERE channel='email' AND ext_id LIKE '<%>'",
// Bulk reports: AI requests sent together through the Message Batches API
"CREATE TABLE IF NOT EXISTS ai_batch (id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,batch_id VARCHAR(80) NULL,lead_id INT UNSIGNED NOT NULL,payload MEDIUMTEXT NULL,params MEDIUMTEXT NULL,result MEDIUMTEXT NULL,tokens_in INT NOT NULL DEFAULT 0,tokens_out INT NOT NULL DEFAULT 0,status VARCHAR(10) NOT NULL DEFAULT 'queued',error TEXT NULL,created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,sent_at DATETIME NULL,KEY st (status),KEY b (batch_id),KEY lead_id (lead_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
];
