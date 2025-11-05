-- ==============================================================================
-- Sample Data for Testing
-- ==============================================================================
-- This file contains a small set of sample words for testing the API
-- For a full database, use the data processing pipeline or import a dump
-- ==============================================================================

-- Sample words
INSERT INTO words (headword, headword_normalized, cefr_level, cefr_source, oxford_level, cet_level, frequency_rank, frequency_count, collins_stars, translation_zh) VALUES
('hello', 'hello', 1, 'oxford', 1, 0, 234, 125678, 5, '你好；喂'),
('world', 'world', 2, 'oxford', 1, 0, 456, 98765, 5, '世界；地球'),
('run', 'run', 1, 'oxford', 1, 0, 156, 245678, 5, '跑；运行；经营'),
('learn', 'learn', 1, 'oxford', 1, 1, 512, 187654, 5, '学习；学会'),
('program', 'program', 2, 'oxford', 1, 0, 789, 76543, 4, '程序；节目'),
('go after', 'goafter', 3, 'cefrj', 0, 0, 1234, 5678, 3, '追求；设法得到'),
('let go', 'letgo', 2, 'oxford', 0, 0, 2345, 4321, 4, '放手；释放'),
('programming', 'programming', 3, 'cefrj', 0, 0, 1567, 34567, 3, '编程；程序设计'),
('computer', 'computer', 2, 'oxford', 1, 1, 345, 156789, 5, '计算机；电脑'),
('software', 'software', 3, 'oxford', 2, 0, 678, 87654, 4, '软件')
ON CONFLICT (headword) DO NOTHING;

-- Sample pronunciations
INSERT INTO pronunciations (word_id, accent, ipa, is_primary) VALUES
(1, 1, '/həˈləʊ/', true),  -- hello, British
(1, 2, '/həˈloʊ/', true),  -- hello, American
(2, 1, '/wɜːld/', true),   -- world, British
(2, 2, '/wɜːrld/', true),  -- world, American
(3, 1, '/rʌn/', true),     -- run, British
(3, 2, '/rʌn/', true),     -- run, American
(4, 1, '/lɜːn/', true),    -- learn, British
(4, 2, '/lɜːrn/', true),   -- learn, American
(5, 1, '/ˈprəʊɡræm/', true),  -- program, British
(5, 2, '/ˈproʊɡræm/', true)   -- program, American
ON CONFLICT (word_id, accent, ipa) DO NOTHING;

-- Sample senses
INSERT INTO senses (word_id, pos, definition_en, definition_zh, sense_order, cefr_level, oxford_level) VALUES
(1, 9, 'Used as a greeting or to begin a phone conversation.', '用作问候语或开始电话交谈。', 1, 1, 1),
(2, 1, 'The earth, together with all of its countries and peoples.', '地球，连同其所有国家和人民。', 1, 2, 1),
(3, 2, 'Move at a speed faster than a walk, never having both or all the feet on the ground at the same time.', '以比步行更快的速度移动，从不让两只或所有脚同时着地。', 1, 1, 1),
(3, 1, 'An act or spell of running.', '跑步；奔跑。', 2, 2, 1),
(4, 2, 'Gain or acquire knowledge of or skill in (something) by study, experience, or being taught.', '通过学习、经验或被教导获得或获取（某事物）的知识或技能。', 1, 1, 1),
(5, 1, 'A planned series of future events or performances.', '计划的未来事件或表演系列。', 1, 2, 1),
(5, 2, 'Provide (a computer or other machine) with coded instructions for the automatic performance of a task.', '为（计算机或其他机器）提供编码指令以自动执行任务。', 2, 3, 1)
ON CONFLICT (word_id, pos, sense_order) DO NOTHING;

-- Sample examples
INSERT INTO examples (sense_id, sentence_en, sentence_zh, example_order) VALUES
(1, 'Hello, how are you?', '你好，你好吗？', 1),
(2, 'The world is a beautiful place.', '世界是个美丽的地方。', 1),
(3, 'I run every morning.', '我每天早上跑步。', 1),
(3, 'She ran to catch the bus.', '她跑着去赶公共汽车。', 2),
(5, 'We need to learn from our mistakes.', '我们需要从错误中学习。', 1),
(6, 'What''s the program for today?', '今天的计划是什么？', 1),
(7, 'I''m learning to program in Go.', '我正在学习用 Go 编程。', 1)
ON CONFLICT (sense_id, example_order) DO NOTHING;

-- Sample word variants
INSERT INTO word_variants (word_id, variant_text, headword_normalized, kind, form_type, frequency_rank, frequency_count) VALUES
(3, 'running', 'running', 1, 4, 892, 45678),     -- run -> running (gerund)
(3, 'ran', 'ran', 1, 2, 1024, 38765),            -- run -> ran (past)
(3, 'runs', 'runs', 1, 5, 756, 52341),           -- run -> runs (3rd person)
(4, 'learning', 'learning', 1, 4, 634, 67890),   -- learn -> learning
(4, 'learned', 'learned', 1, 2, 1234, 34567),    -- learn -> learned
(4, 'learnt', 'learnt', 1, 2, 2345, 12345),      -- learn -> learnt (British)
(5, 'programs', 'programs', 1, 1, 1567, 23456),  -- program -> programs
(5, 'programme', 'programme', 2, NULL, 2345, 18765), -- program -> programme (British spelling)
(5, 'programmed', 'programmed', 1, 2, 3456, 8765)    -- program -> programmed
ON CONFLICT (word_id, variant_text, kind, COALESCE(form_type, 0)) DO NOTHING;

-- ==============================================================================
-- Sample data loaded successfully
-- ==============================================================================
-- Verify with: SELECT COUNT(*) FROM words;
-- Test API: curl http://localhost:8080/api/v1/words/hello
-- ==============================================================================
