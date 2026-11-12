"""CDC checks must be real, mandatory and attributable in publishable receipts."""
import copy
import gzip
import io
import json
import os
from pathlib import Path
import subprocess
import signal
import sys
import tempfile
import time
import unittest
from unittest import mock

import cdc_evidence
import cdc_fixture
import ci
import correctness
import full_capture
import record
from test_ci_policy import cdc_receipt, cdc_event


class CDCEvidenceTests(unittest.TestCase):
    def test_initializer_failure_retains_diagnostic_and_never_starts_server(self):
        for failure in (subprocess.CalledProcessError(1,'initializer'),
                        subprocess.TimeoutExpired('initializer',60)):
            with self.subTest(failure=type(failure).__name__):
                programs={name:'/synthetic/'+name for name in ('mariadbd','mariadb-install-db','mariadb')}
                def initialize(command,**kwargs):
                    kwargs['stdout'].write('owned initializer diagnostic\n')
                    raise failure
                stderr=io.StringIO()
                with mock.patch.object(cdc_fixture,'toolchain',return_value=(Path('/synthetic'),programs)), \
                     mock.patch.object(cdc_fixture,'certificates'), \
                     mock.patch.object(cdc_fixture.subprocess,'run',side_effect=initialize), \
                     mock.patch.object(cdc_fixture.subprocess,'Popen') as launch, \
                     mock.patch.object(cdc_fixture.sys,'stderr',stderr):
                    with self.assertRaises(type(failure)):
                        cdc_fixture.main(['synthetic-correctness-child'])
                self.assertIn('owned initializer diagnostic',stderr.getvalue())
                launch.assert_not_called()

    def test_receipt_requires_matching_source_and_exactly_one_event(self):
        receipt = cdc_receipt()
        capture = {'inputs':record.inputs(), 'cdc_fixture':receipt}
        output = json.dumps(cdc_event())
        cdc_evidence.validate(capture, output)
        for changed, log in (({'inputs':capture['inputs']}, ''), (capture, ''),
                             ({'inputs':capture['inputs']}, output), (capture, output+'\n'+output)):
            with self.assertRaises(ValueError):
                cdc_evidence.validate(changed, log)
        for field, value in (('fixture_sha256','stale'), ('server_id',1), ('semisync_available',False),
                             ('transport','shared-server'), ('binlog_format','STATEMENT')):
            changed = copy.deepcopy(capture)
            changed['cdc_fixture'][field] = value
            log = json.dumps({**cdc_event(), 'Output':'CDC_FIXTURE '+json.dumps(changed['cdc_fixture'])})
            with self.assertRaises(ValueError):
                cdc_evidence.validate(changed, log)

    def test_actual_publishers_reject_joint_metadata_and_event_omission(self):
        from test_session_evidence import SessionEvidenceTests
        from test_full_capture import FullCaptureTests
        FullCaptureTests.setUpClass()
        for family in ('correctness', 'full'):
            with self.subTest(family=family), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                if family == 'correctness':
                    capture = SessionEvidenceTests().valid_capture(directory)
                    correctness.validate(capture, directory)
                    names = ('correctness',)
                else:
                    capture, _ = FullCaptureTests().valid_capture(directory)
                    full_capture.validate(capture, directory)
                    names = ('mariadb','race')
                del capture['cdc_fixture']
                for name in names:
                    path = directory/(name+'.log'+('.gz' if family == 'full' else ''))
                    output = gzip.decompress(path.read_bytes()).decode() if family == 'full' else path.read_text()
                    output = '\n'.join(line for line in output.splitlines() if 'CDC_FIXTURE ' not in line)+'\n'
                    if family == 'full':
                        path.write_bytes(gzip.compress(output.encode(), mtime=0))
                        capture['stages'][name]['cases'] = record.test_cases(output)
                    else:
                        path.write_text(output)
                        issues, cases = ci.assess_tests(output, 0)
                        self.assertEqual(issues, [])
                        capture['stages'][name]['cases'] = cases
                    capture['logs'][path.name] = record.digest(path.read_bytes())
                with self.assertRaisesRegex(ValueError,'CDC fixture provenance'):
                    if family == 'correctness':
                        correctness.summarize(capture, directory)
                    else:
                        full_capture.validate(capture, directory)

    def test_fixture_uses_only_owned_socket_server_and_waits_for_cleanup(self):
        calls = []
        server = mock.Mock()
        server.poll.return_value = None
        child = mock.Mock(pid=424242)
        child.poll.return_value = 0
        child.returncode = 0

        def run(command, **kwargs):
            calls.append(command)
            text = '12.3.3-MariaDB' if kwargs.get('input') == 'SELECT VERSION()' else '1'
            return subprocess.CompletedProcess(command,0,text,'')

        def launch(command, **kwargs):
            calls.append(command)
            if command[0].endswith('mariadbd'):
                self.assertIn('--skip-networking',command)
                self.assertTrue(any(arg.startswith('--datadir=') and 'go-mariadb-cdc-' in arg for arg in command))
                return server
            self.assertTrue('go-mariadb-cdc-' in kwargs['env']['GO_MARIADB_CDC_SOCKET'])
            return child

        programs = {name:'/synthetic/bin/'+name for name in ('mariadbd','mariadb-install-db','mariadb')}
        with mock.patch.object(cdc_fixture,'toolchain',return_value=(Path('/synthetic'),programs)), \
             mock.patch.object(cdc_fixture.os,'killpg',side_effect=ProcessLookupError), \
             mock.patch.object(cdc_fixture,'certificates'), \
             mock.patch.object(cdc_fixture.subprocess,'run',side_effect=run), \
             mock.patch.object(cdc_fixture.subprocess,'Popen',side_effect=launch):
            self.assertEqual(cdc_fixture.main(['--','synthetic-correctness-child']),0)
        server.terminate.assert_called_once()
        server.wait.assert_called_once_with(timeout=10)
        self.assertTrue(all('--protocol=socket' in command for command in calls if command[0].endswith('/mariadb')))

    def test_stop_kills_and_reaps_only_the_supplied_process_after_timeout(self):
        process = mock.Mock()
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired('owned-child',10),0]
        cdc_fixture.stop(process)
        process.terminate.assert_called_once()
        process.kill.assert_called_once()
        self.assertEqual(process.wait.call_count,2)

    def test_correctness_children_use_only_their_owned_process_group(self):
        process = mock.Mock(pid=424242)
        process.poll.return_value = None
        with mock.patch.object(cdc_fixture.os,'killpg',side_effect=[None,ProcessLookupError,None]) as kill:
            cdc_fixture.stop(process, group=True)
        self.assertEqual(kill.call_args_list,[mock.call(424242,signal.SIGTERM),
                         mock.call(424242,0),mock.call(424242,signal.SIGKILL)])
        process.terminate.assert_not_called()

    def test_server_cleanup_runs_even_when_child_cleanup_raises(self):
        server,child=mock.Mock(),mock.Mock()
        child.poll.return_value=0;child.returncode=0
        programs={name:'/synthetic/'+name for name in ('mariadbd','mariadb-install-db','mariadb')}
        def sql(command,**kwargs):
            value='12.3.3-MariaDB' if kwargs.get('input')=='SELECT VERSION()' else '1'
            return subprocess.CompletedProcess(command,0,value,'')
        with mock.patch.object(cdc_fixture,'toolchain',return_value=(Path('/synthetic'),programs)), \
             mock.patch.object(cdc_fixture,'certificates'), \
             mock.patch.object(cdc_fixture.subprocess,'run',side_effect=sql), \
             mock.patch.object(cdc_fixture.subprocess,'Popen',side_effect=[server,child]), \
             mock.patch.object(cdc_fixture,'stop',side_effect=[RuntimeError('child cleanup'),None]) as stop:
            with self.assertRaisesRegex(RuntimeError,'child cleanup'):
                cdc_fixture.main(['synthetic-correctness-child'])
        self.assertEqual(stop.call_args_list,[mock.call(child,group=True),mock.call(server)])

    def test_owned_descendants_stop_after_leader_exit_or_ignored_term(self):
        for running,ignore in ((False,False),(True,True)):
            with self.subTest(running=running),tempfile.TemporaryDirectory(prefix='go-mariadb-cdc-cleanup-') as temporary:
                marker=Path(temporary)/'worker'
                worker=('import os,signal,time\n'+
                        ('signal.signal(signal.SIGTERM,signal.SIG_IGN)\n' if ignore else '')+
                        f'p={str(marker)!r}\nopen(p,"w").close()\n'+
                        'while True:\n os.utime(p,None)\n time.sleep(0.01)\n')
                leader=('import subprocess,sys,time\nfrom pathlib import Path\n'+
                        f'child=subprocess.Popen([sys.executable,"-c",{worker!r}])\n'+
                        f'p=Path({str(marker)!r})\ndeadline=time.monotonic()+5\n'+
                        'while not p.exists():\n if time.monotonic()>deadline: raise RuntimeError("worker readiness")\n time.sleep(0.01)\n'+
                        'print(child.pid,flush=True)\n'+('time.sleep(60)\n' if running else ''))
                owner=subprocess.Popen([sys.executable,'-c',leader],start_new_session=True,
                                       stdout=subprocess.PIPE,text=True)
                try:
                    self.assertTrue(owner.stdout.readline().strip().isdigit())
                    owner.stdout.close()
                    if not running: owner.wait(timeout=5)
                    cdc_fixture.stop(owner,group=True,grace=0.1)
                    # Orphaned zombies may await init's reap on Linux. Assert
                    # that no descendant executes, rather than kill(pid,0).
                    before=marker.stat().st_mtime_ns
                    time.sleep(0.1)
                    self.assertEqual(marker.stat().st_mtime_ns,before,'owned worker survived cleanup')
                    self.assertIsNotNone(owner.poll())
                finally:
                    try: os.killpg(owner.pid,signal.SIGKILL)
                    except ProcessLookupError: pass
                    owner.wait(timeout=5)
                    if not owner.stdout.closed: owner.stdout.close()


class CDCToolchainTests(unittest.TestCase):
    def executable(self,root,relative):
        path=root/relative;path.parent.mkdir(parents=True,exist_ok=True)
        path.write_text('#!/bin/sh\nexit 99\n');path.chmod(0o700)
        return path

    def test_standalone_initializer_uses_server_prefix_not_unrelated_path(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)/'selected';other=Path(temporary)/'other'
            for name in ('bin/mariadbd','bin/mariadb','scripts/mariadb-install-db'):
                self.executable(root,name)
            self.executable(other,'bin/mariadb-install-db')
            with mock.patch.dict(os.environ,{'PATH':str(root/'bin')+os.pathsep+str(other/'bin'),
                                           'GO_MARIADB_CDC_BASEDIR':''}):
                basedir,programs=cdc_fixture.toolchain()
            self.assertEqual(basedir,root.resolve())
            self.assertEqual(programs['mariadb-install-db'],str(root.resolve()/'scripts/mariadb-install-db'))

    def test_system_layout_and_server_symlink(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)/'usr';links=Path(temporary)/'links';links.mkdir()
            server=self.executable(root,'sbin/mariadbd')
            for name in ('bin/mariadb','bin/mysql_install_db'):self.executable(root,name)
            (links/'mariadbd').symlink_to(server)
            with mock.patch.dict(os.environ,{'PATH':str(links),'GO_MARIADB_CDC_BASEDIR':''}):
                basedir,programs=cdc_fixture.toolchain()
            self.assertEqual(basedir,root.resolve())
            self.assertEqual(programs['mariadb-install-db'],str(root.resolve()/'bin/mysql_install_db'))

    def test_explicit_basedir_selects_complete_matching_installation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)/'selected installation'
            for name in ('libexec/mariadbd','bin/mariadb','bin/mariadb-install-db'):
                self.executable(root,name)
            with mock.patch.dict(os.environ,{'PATH':'/unrelated','GO_MARIADB_CDC_BASEDIR':str(root)}):
                basedir,programs=cdc_fixture.toolchain()
            self.assertEqual(basedir,root.resolve())
            self.assertTrue(all(Path(path).is_relative_to(root.resolve()) for path in programs.values()))

    def test_incomplete_installation_does_not_fall_back_to_path(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)/'selected';other=Path(temporary)/'other'
            for name in ('bin/mariadbd','bin/mariadb'):self.executable(root,name)
            self.executable(other,'bin/mariadb-install-db')
            with mock.patch.dict(os.environ,{'PATH':str(other/'bin'),'GO_MARIADB_CDC_BASEDIR':str(root)}):
                with self.assertRaisesRegex(RuntimeError,'Incomplete MariaDB CDC toolchain'):
                    cdc_fixture.toolchain()
