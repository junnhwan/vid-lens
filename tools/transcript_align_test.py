import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
import sys
sys.path.insert(0, str(Path(__file__).parent))

spec = importlib.util.spec_from_file_location("align", Path(__file__).with_name("transcript_align.py"))
align = importlib.util.module_from_spec(spec)
spec.loader.exec_module(align)


def item(text, start, end):
    return SimpleNamespace(text=text, start_time=start, end_time=end)


class AlignmentSourceTest(unittest.TestCase):
    def test_repeated_words_keep_their_actual_source_positions(self):
        words = align.source_words("可以。可以。", [item("可", 0.1, 0.2), item("以", 0.2, 0.3), item("可", 1, 1.1), item("以", 1.1, 1.2)], 18000, 2000)
        self.assertEqual([(w["text_start"], w["start_ms"]) for w in words], [(0, 18100), (1, 18200), (3, 19000), (4, 19100)])

    def test_quantized_final_character_stays_in_its_sentence(self):
        words = align.source_words("回来。但", [item("回", .1, .2), item("来", .2, .2), item("但", .5, .6)], 0, 1000)
        self.assertEqual([w["text"] for w in words], ["回来", "但"])
        self.assertEqual(words[0]["end_ms"], 200)

    def test_invalid_or_missing_acoustic_words_cannot_become_precise(self):
        for items in [[item("改", .1, .2)], [], [item("原", .1, 9)]]:
            with self.assertRaises(ValueError):
                align.source_words("原文", items, 0, 2000)


class ModelIdentityTest(unittest.TestCase):
    def test_same_path_replacement_and_runtime_changes_isolate_cache(self):
        import tempfile
        from alignment_model import prepare, validate, ModelIdentityError
        with tempfile.TemporaryDirectory(prefix='model with spaces ') as directory:
            root = Path(directory)
            (root/'config.json').write_text('{}')
            (root/'model.safetensors').write_bytes(b'original')
            first = prepare(root, 'a'*40, 'qwen')
            self.assertEqual(validate(root, 'qwen')['identity'], first['identity'])
            key = align.cache_identity(first,'qwen','Chinese',{'torch':'1'},'audio',0,1000,'原文')
            (root/'model.safetensors').write_bytes(b'replaced')
            with self.assertRaises(ModelIdentityError): validate(root,'qwen')
            second = prepare(root, 'a'*40, 'qwen')
            self.assertNotEqual(key, align.cache_identity(second,'qwen','Chinese',{'torch':'1'},'audio',0,1000,'原文'))
            self.assertNotEqual(key, align.cache_identity(first,'qwen','Chinese',{'torch':'2'},'audio',0,1000,'原文'))
            with self.assertRaises(ModelIdentityError): validate(root,'mlx')

    def test_unprepared_remote_name_cannot_download(self):
        from alignment_model import validate, ModelIdentityError
        with self.assertRaises(ModelIdentityError): validate('Qwen/absent-local-model','qwen')

if __name__ == "__main__":
    unittest.main()
