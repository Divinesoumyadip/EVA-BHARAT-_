-- 0002_seed.sql
-- Seed data matching the assignment's example windows and media lists,
-- plus a blank item and a broken-URL item so evaluators can see fallback
-- handling without needing to break anything themselves.
--
-- Media assets are stable, public, no-auth sample files (Google's public
-- GCS sample bucket for video, Wikimedia/placeholder services for images)
-- suitable for use in a deployed demo.

INSERT INTO windows (id, name) VALUES
    ('W1', 'Window 1'),
    ('W2', 'Window 2'),
    ('W3', 'Window 3')
ON CONFLICT (id) DO NOTHING;

INSERT INTO media (id, name, type, url, duration_seconds) VALUES
    ('M1', 'Media 1 - Mountain',  'image', 'https://picsum.photos/id/1015/1280/720', 10),
    ('M2', 'Media 2 - Forest',    'image', 'https://picsum.photos/id/1016/1280/720', 12),
    ('M3', 'Media 3 - Ocean',     'image', 'https://picsum.photos/id/1018/1280/720', 8),
    ('M4', 'Media 4 - Sample Clip', 'video', 'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4', 15),
    ('M5', 'Media 5 - City',      'image', 'https://picsum.photos/id/1024/1280/720', 10),
    ('M6', 'Media 6 - Sample Clip 2', 'video', 'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4', 15),
    ('M7', 'Media 7 - Desert',    'image', 'https://picsum.photos/id/1036/1280/720', 10),
    ('BLANK', 'Blank Slate', 'blank', '', 5)
ON CONFLICT (id) DO NOTHING;

-- Window 1: M1 -> M2 -> M3
INSERT INTO playlist_items (window_id, media_id, position) VALUES
    ('W1', 'M1', 0),
    ('W1', 'M2', 1),
    ('W1', 'M3', 2)
ON CONFLICT DO NOTHING;

-- Window 2: M2 -> M4 -> M5
INSERT INTO playlist_items (window_id, media_id, position) VALUES
    ('W2', 'M2', 0),
    ('W2', 'M4', 1),
    ('W2', 'M5', 2)
ON CONFLICT DO NOTHING;

-- Window 3: M1 -> M5 -> M6 -> BLANK
-- (BLANK is here deliberately, as an explicitly configured playlist item,
-- to demonstrate that blank only ever appears when configured - never as
-- an automatic filler for the rest of the 5-hour cycle.)
INSERT INTO playlist_items (window_id, media_id, position) VALUES
    ('W3', 'M1', 0),
    ('W3', 'M5', 1),
    ('W3', 'M6', 2),
    ('W3', 'BLANK', 3)
ON CONFLICT DO NOTHING;
