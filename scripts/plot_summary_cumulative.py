#!/usr/bin/env python3
"""Render standalone experimental figures with matplotlib (optional dependency)."""
import csv,json,sys
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
p=Path(sys.argv[1]);rows=list(csv.DictReader((p/'cumulative.csv').open()))
fig,axes=plt.subplots(2,2,figsize=(12,8),layout='constrained')
for scenario in (0,1):
 for group in 'ABC':
  selected=[r for r in rows if int(r['scenario'])==scenario and r['group']==group]
  axes[scenario,0].plot([int(r['round'])+1 for r in selected],[float(r['cumulative_input'])+float(r['cumulative_output']) if r['cumulative_input'] and r['cumulative_output'] else float('nan') for r in selected],marker='o',label=group)
  axes[scenario,1].plot([int(r['round'])+1 for r in selected],[int(r['fields_correct'])/6 for r in selected],marker='o',label=group)
 axes[scenario,0].set(title=f'Session {scenario}: cumulative input + output tokens',xlabel='Replay turn',ylabel='Tokens')
 axes[scenario,1].set(title=f'Session {scenario}: exact field accuracy',xlabel='Replay turn',ylabel='Accuracy',ylim=(-.05,1.05))
 for ax in axes[scenario]:ax.legend();ax.grid(alpha=.3)
fig.savefig(p/'comparison.png',dpi=180)
fig.savefig(p/'comparison.svg')
