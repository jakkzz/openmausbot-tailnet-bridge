"""Normal terminal fallback; synthetic data, no native app/pairing access."""
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('show_window',Path(__file__).with_name('show-invite.py'))
show=importlib.util.module_from_spec(spec);spec.loader.exec_module(show)

class TTY(io.StringIO):
    def isatty(self): return True

@unittest.skipUnless(sys.platform == 'darwin', 'macOS native PNG viewer')
class WindowQRTests(unittest.TestCase):
    def test_normal_terminal_preflight_succeeds_without_resize(self):
        out=TTY()
        with patch.dict(os.environ,{'TERM_PROGRAM':'herdr'},clear=True),patch.object(show,'renderer',return_value=Path('unused')),patch.object(show,'preview_available',return_value=True),patch.object(show.shutil,'get_terminal_size',return_value=os.terminal_size((80,24))),patch.object(sys,'argv',['show-invite.py','--preflight']),patch.object(sys,'stdout',out),patch.object(show.subprocess,'run') as run:
            self.assertEqual(show.main(),0)
            run.assert_not_called()
        self.assertIn('No resizing needed',out.getvalue())
        self.assertNotIn('enlarge',out.getvalue())

    def test_auto_fits_both_terminals_and_explicit_mode_is_respected(self):
        required={'columns':158,'rows':79}
        with patch.dict(os.environ,{'TERM_PROGRAM':'herdr'},clear=True):
            self.assertEqual(show.fitted_format('auto',os.terminal_size((80,24)),required,True),'window')
            self.assertEqual(show.fitted_format('auto',os.terminal_size((180,90)),required,True),'ansi')
            self.assertEqual(show.fitted_format('ansi',os.terminal_size((80,24)),required,True),'ansi')
        with patch.dict(os.environ,{'TERM_PROGRAM':'iTerm.app'},clear=True):
            self.assertEqual(show.fitted_format('auto',os.terminal_size((80,24)),required,True),'window')
            self.assertEqual(show.fitted_format('auto',os.terminal_size((80,43)),required,True),'png')

    def test_viewer_gets_persistent_private_image_and_link_stays_one_line(self):
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp)/'invite.txt';link='openmausbot://pair?token=SYNTHETIC&code=123456';source.write_text(link)
            out=TTY();calls=[];images=[]
            def run(args,**kwargs):
                calls.append(args)
                if '--png' in args:
                    png=Path(args[-1]);png.write_bytes(b'synthetic image fixture');images.append(png)
                return subprocess.CompletedProcess(args,0)
            with patch.object(show,'renderer',return_value=Path('renderer')),patch.object(show,'preview_available',return_value=True),patch.object(sys,'argv',['show-invite.py',str(source),'--format','window']),patch.object(sys,'stdout',out),patch.object(show.subprocess,'run',side_effect=run):
                self.assertEqual(show.main(),0)
            self.assertEqual([line for line in out.getvalue().splitlines() if line.startswith('openmausbot://')],[link])
            self.assertEqual(len(images),1)
            self.assertTrue(images[0].exists())
            self.assertEqual(images[0].stat().st_mode & 0o777,0o600)
            self.assertEqual(images[0].parent.stat().st_mode & 0o777,0o700)
            self.assertEqual(calls[-1][:3],['/usr/bin/open','-a','Preview'])
            self.assertIn('No terminal resize needed',out.getvalue())

if __name__=='__main__':unittest.main()
