<?php
// Export a Setu v1 install for import into Setu v2 (multi-tenant).
// Run on the v1 server:  php tools/export-v2.php > setu-v1-export.jsonl
// Then on v2:            setu import-v1 setu-v1-export.jsonl
// The file contains decrypted API keys and password hashes – transfer it securely and delete it afterwards.
if(PHP_SAPI!=='cli')exit("Run from the command line.\n");
require __DIR__.'/../app/bootstrap.php';
$out=fopen('php://stdout','w');
$w=function($t,$r)use($out){fwrite($out,json_encode(['t'=>$t,'r'=>$r],JSON_UNESCAPED_UNICODE|JSON_UNESCAPED_SLASHES|JSON_INVALID_UTF8_SUBSTITUTE)."\n");};
$w('meta',['version'=>APP_VERSION,'timezone'=>setting('timezone','Asia/Kolkata'),'exported_at'=>date('c')]);
foreach(DB::all('SELECT k,v FROM settings') as $r){if(preg_match(Settings::SECRET,$r['k']))$r['v']=Settings::dec($r['v']);$w('settings',$r);}
$tables=['users','campaigns','fields','sources','prompts','assets','leads','automations','reports','events','messages','automation_runs','appointments','calls','api_usage','research_cache'];
foreach($tables as $t){
  try{$st=DB::pdo()->query("SELECT * FROM `$t`",PDO::FETCH_ASSOC);}catch(Throwable $e){continue;}
  while($r=$st->fetch())$w($t,$r);}
fwrite(STDERR,"Export finished.\n");
