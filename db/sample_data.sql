-- ==============================================================================
-- Sample Data for Testing
-- ==============================================================================
-- This file contains a small set of sample words for testing the API
-- Load this file only into an empty disposable database after schema/index setup.
-- It is intentionally insert-only so re-imports fail closed instead of overwriting rows.
-- For a full database, use the data processing pipeline or import a dump
-- ==============================================================================

BEGIN;

-- Sample words
WITH sample_words (headword, headword_normalized, cefr_level, cefr_source, oxford_level, cet_level, school_level, frequency_rank, frequency_count, collins_stars, translation_zh) AS (
	VALUES
		('hello', 'hello', 1, 'oxford', 1, 0, 1, 234, 125678, 5, '你好；喂'),
		('world', 'world', 2, 'oxford', 1, 0, 1, 456, 98765, 5, '世界；地球'),
		('run', 'run', 1, 'oxford', 1, 0, 1, 156, 245678, 5, '跑；运行；经营'),
		('learn', 'learn', 1, 'oxford', 1, 1, 2, 512, 187654, 5, '学习；学会'),
		('program', 'program', 2, 'oxford', 1, 0, 2, 789, 76543, 4, '程序；节目'),
		('go after', 'goafter', 3, 'cefrj', 0, 0, 3, 1234, 5678, 3, '追求；设法得到'),
		('let go', 'letgo', 2, 'oxford', 0, 0, 2, 2345, 4321, 4, '放手；释放'),
		('programming', 'programming', 3, 'cefrj', 0, 0, 3, 1567, 34567, 3, '编程；程序设计'),
		('computer', 'computer', 2, 'oxford', 1, 1, 2, 345, 156789, 5, '计算机；电脑'),
		('software', 'software', 3, 'oxford', 2, 0, 3, 678, 87654, 4, '软件')
)
INSERT INTO words (headword, headword_normalized, cefr_level, cefr_source, oxford_level, cet_level, school_level, frequency_rank, frequency_count, collins_stars, translation_zh)
SELECT headword, headword_normalized, cefr_level, cefr_source, oxford_level, cet_level, school_level, frequency_rank, frequency_count, collins_stars, translation_zh
FROM sample_words;

-- Sample pronunciations
WITH sample_pronunciations (headword, accent, ipa, is_primary) AS (
	VALUES
		('hello', 1, '/həˈləʊ/', true),
		('hello', 2, '/həˈloʊ/', true),
		('world', 1, '/wɜːld/', true),
		('world', 2, '/wɜːrld/', true),
		('run', 1, '/rʌn/', true),
		('run', 2, '/rʌn/', true),
		('learn', 1, '/lɜːn/', true),
		('learn', 2, '/lɜːrn/', true),
		('program', 1, '/ˈprəʊɡræm/', true),
		('program', 2, '/ˈproʊɡræm/', true)
)
INSERT INTO pronunciations (word_id, accent, ip_a, is_primary)
SELECT words.id, sample_pronunciations.accent, sample_pronunciations.ipa, sample_pronunciations.is_primary
FROM sample_pronunciations
JOIN words ON words.headword = sample_pronunciations.headword;

-- Sample senses
WITH sample_senses (headword, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level) AS (
	VALUES
		('hello', 9, 'Used as a greeting or to begin a phone conversation.', '用作问候语或开始电话交谈。', 1, 1, 'oxford', 1),
		('world', 1, 'The earth, together with all of its countries and peoples.', '地球，连同其所有国家和人民。', 1, 2, 'oxford', 1),
		('run', 2, 'Move at a speed faster than a walk, never having both or all the feet on the ground at the same time.', '以比步行更快的速度移动，从不让两只或所有脚同时着地。', 1, 1, 'oxford', 1),
		('run', 1, 'An act or spell of running.', '跑步；奔跑。', 2, 2, 'oxford', 1),
		('learn', 2, 'Gain or acquire knowledge of or skill in (something) by study, experience, or being taught.', '通过学习、经验或被教导获得或获取（某事物）的知识或技能。', 1, 1, 'oxford', 1),
		('program', 1, 'A planned series of future events or performances.', '计划的未来事件或表演系列。', 1, 2, 'oxford', 1),
		('program', 2, 'Provide (a computer or other machine) with coded instructions for the automatic performance of a task.', '为（计算机或其他机器）提供编码指令以自动执行任务。', 2, 3, 'oxford', 1)
)
INSERT INTO senses (word_id, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level)
SELECT words.id, sample_senses.pos, sample_senses.definition_en, sample_senses.definition_zh, sample_senses.sense_order, sample_senses.cefr_level, sample_senses.cefr_source, sample_senses.oxford_level
FROM sample_senses
JOIN words ON words.headword = sample_senses.headword;

-- Sample examples
WITH sample_examples (headword, pos, sense_order, sentence_en, sentence_zh, example_order) AS (
	VALUES
		('hello', 9, 1, 'Hello, how are you?', '你好，你好吗？', 1),
		('world', 1, 1, 'The world is a beautiful place.', '世界是个美丽的地方。', 1),
		('run', 2, 1, 'I run every morning.', '我每天早上跑步。', 1),
		('run', 2, 1, 'She ran to catch the bus.', '她跑着去赶公共汽车。', 2),
		('learn', 2, 1, 'We need to learn from our mistakes.', '我们需要从错误中学习。', 1),
		('program', 1, 1, 'What''s the program for today?', '今天的计划是什么？', 1),
		('program', 2, 2, 'I''m learning to program in Go.', '我正在学习用 Go 编程。', 1)
)
INSERT INTO examples (sense_id, sentence_en, sentence_zh, example_order)
SELECT senses.id, sample_examples.sentence_en, sample_examples.sentence_zh, sample_examples.example_order
FROM sample_examples
JOIN words ON words.headword = sample_examples.headword
JOIN senses ON senses.word_id = words.id AND senses.pos = sample_examples.pos AND senses.sense_order = sample_examples.sense_order;

-- Sample word variants
-- form_type semantics follow isdict-commons: 1=past, 2=past_participle,
-- 3=present_3rd, 4=gerund, 5=plural
WITH sample_variants (headword, variant_text, headword_normalized, kind, form_type, frequency_rank, frequency_count) AS (
	VALUES
		('run', 'running', 'running', 1, 4, 892, 45678),
		('run', 'ran', 'ran', 1, 1, 1024, 38765),
		('run', 'runs', 'runs', 1, 3, 756, 52341),
		('learn', 'learning', 'learning', 1, 4, 634, 67890),
		('learn', 'learned', 'learned', 1, 2, 1234, 34567),
		('learn', 'learnt', 'learnt', 1, 1, 2345, 12345),
		('program', 'programs', 'programs', 1, 5, 1567, 23456),
		('program', 'programme', 'programme', 2, NULL, 2345, 18765),
		('program', 'programmed', 'programmed', 1, 1, 3456, 8765)
)
INSERT INTO word_variants (word_id, variant_text, headword_normalized, kind, form_type, frequency_rank, frequency_count)
SELECT words.id, sample_variants.variant_text, sample_variants.headword_normalized, sample_variants.kind, sample_variants.form_type, sample_variants.frequency_rank, sample_variants.frequency_count
FROM sample_variants
JOIN words ON words.headword = sample_variants.headword;

COMMIT;

-- ==============================================================================
-- Sample data loaded successfully
-- ==============================================================================
-- Verify with: SELECT COUNT(*) FROM words;
-- Test API: curl http://localhost:8080/api/v1/words/hello
-- ==============================================================================
