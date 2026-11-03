<?php
// Thrown by a job that has to wait (e.g. for shared research another job is doing): it runs again after getCode() seconds
final class Defer extends Exception{}
