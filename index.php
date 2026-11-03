<?php
require __DIR__.'/app/bootstrap.php';
// Run pending database migrations right after an update (before anything reads new columns)
// (version + number of migration files, so new migrations also run when the version number stays the same)
$dbv=APP_VERSION.'#'.count(glob(APP.'/migrations/*.php'));
if(setting('db_version')!==$dbv){try{Migrator::run();Settings::set('db_version',$dbv);}catch(Throwable $e){logmsg('Migration: '.$e->getMessage());}}
$r=preg_replace('/[^a-z_]/','',$_GET['r']??'dashboard');
if(!is_file(APP.'/pages/'.$r.'.php'))$r='dashboard';
if($r!=='login')require_login();
if(is_post())csrf_check();
// Page access: [permission, module]
$acl=['emails'=>['leads','crm'],'scraper'=>['prospecting','prospecting'],'campaign'=>['prospecting','prospecting'],'campaigns'=>['prospecting','prospecting'],'prospects'=>['prospecting','prospecting'],'reports'=>['prospecting','prospecting'],'automations'=>['automations','crm'],'automation'=>['automations','crm'],'templates'=>['templates','crm'],'prompts'=>['templates','crm'],'ai'=>['settings','crm'],'settings'=>['settings','crm'],'updates'=>['settings','crm'],'usage'=>['settings','crm'],'team'=>['team','crm'],'modules'=>['modules','crm'],'recipes'=>['automations','crm'],'user'=>['team','crm']];
if(isset($acl[$r])){[$perm,$m]=$acl[$r];if(!can($perm))denied();if(!mod($m))denied('The '.modules()[$m][0].' module is switched off. A super admin can switch it on under Modules in the left menu.');}
header('X-Frame-Options: SAMEORIGIN');header('X-Content-Type-Options: nosniff');header('Referrer-Policy: strict-origin-when-cross-origin');
$title='Setu';$subtitle='';$active=$r;
ob_start();
require APP.'/pages/'.$r.'.php';
$content=ob_get_clean();
require APP.'/views/layout.php';
if(!setting('cron_tried')){Settings::set('cron_tried',1);Cron::install();}
if(Cron::due()){session_write_close();if(function_exists('fastcgi_finish_request'))fastcgi_finish_request();elseif(function_exists('litespeed_finish_request'))litespeed_finish_request();Cron::kick();}
