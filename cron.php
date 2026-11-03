<?php
// Background scheduler. Runs automatically (server crontab or built-in web scheduler).
// Manual options:  php /path/to/cron.php   or   https://your-site/cron.php?key=CRON_KEY
require __DIR__.'/app/bootstrap.php';
$cli=PHP_SAPI==='cli';
$chain=!$cli&&isset($_GET['chain']);
if(!$cli){
  session_write_close();
  if(!hash_equals(setting('cron_key'),(string)($_GET['key']??''))){http_response_code(403);exit('Forbidden');}
  if($chain)Cron::detach();}
if($cli)Settings::set('cron_cli_last',time());
// Several workers can run side by side (Settings → Background scheduler → Parallel workers): slot 0 is the main
// scheduler; extra slots only work through due jobs (e.g. several AI reports at once) and then exit
$fp=null;$slot=-1;
for($i=0;$i<Cron::workers();$i++){$f=fopen(Cron::lock($i),'c');if(flock($f,LOCK_EX|LOCK_NB)){$fp=$f;$slot=$i;break;}fclose($f);}
if(!$fp)exit($cli?"All workers busy\n":'');
$t0=time();
Settings::set('cron_last',$t0);
$span=$cli?50:($chain?50:25);
$n=0;
// Keep working (and waiting for scheduled jobs) for about a minute
while(time()-$t0<$span){
  $done=Queue::work(max(5,$span-(time()-$t0)));
  $n+=$done;
  if(!$done){if($slot>0||(!$chain&&!$cli))break;sleep(5);}
  Settings::set('cron_last',time());}
if($slot>0){flock($fp,LOCK_UN);fclose($fp);exit($cli?"Processed $n job(s)\n":'');}
try{Automation::scan();Booking::remind();}catch(Throwable $e){logmsg('Scheduler: '.$e->getMessage());}
$rep=0;
if(Mailer::imapOn()&&time()-(int)setting('imap_last','0')>120){
  Settings::set('imap_last',time());
  try{$rep=Outreach::checkReplies();}catch(Throwable $e){logmsg('IMAP: '.$e->getMessage());}}
try{Updater::check();}catch(Throwable $e){}
foreach(glob(STORAGE.'/tmp/upd-*',GLOB_ONLYDIR) as $d)if(filemtime($d)<time()-86400)Updater::rrm($d);
if(mt_rand(1,30)===1){DB::q("DELETE FROM jobs WHERE status='done' AND created_at<NOW()-INTERVAL 14 DAY");DB::q("DELETE FROM job_steps WHERE created_at<NOW()-INTERVAL 30 DAY");}
flock($fp,LOCK_UN);fclose($fp);
// Web scheduler: start the next round unless a real server cron is active
if($chain&&time()-(int)DB::val("SELECT v FROM settings WHERE k='cron_cli_last'")>180&&DB::val("SELECT v FROM settings WHERE k='scheduler'")!=='off'){Settings::set('cron_kick',time());Cron::async(rtrim(setting('cron_url',base_url('')),'/').'/cron.php?key='.urlencode(setting('cron_key')).'&chain=1');}
if($cli)echo "Processed $n job(s), $rep new repl(ies)\n";
