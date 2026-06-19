export function getLastSevenDays() {
  const dates = [];
  for (let i = 6; i >= 0; i--) {
    const d = new Date();
    d.setDate(d.getDate() - i);
    const month = '' + (d.getMonth() + 1);
    const day = '' + d.getDate();
    const year = d.getFullYear();

    const formattedDate = [year, month.padStart(2, '0'), day.padStart(2, '0')].join('-');
    dates.push(formattedDate);
  }
  return dates;
}

export function getTodayDay() {
  let today = new Date();
  return today.toISOString().slice(0, 10);
}

export function generateLineChartOptions(data, unit) {
  const dates = data.map((item) => item.date);
  const values = data.map((item) => item.value);

  const minDate = dates[0];
  const maxDate = dates[dates.length - 1];

  const minValue = Math.min(...values);
  const maxValue = Math.max(...values);

  return {
    series: [
      {
        data: values
      }
    ],
    type: 'line',
    height: 90,
    options: {
      chart: {
        sparkline: {
          enabled: true
        },
        background: 'transparent'
      },
      dataLabels: {
        enabled: false
      },
      colors: ['#fff'],
      fill: {
        type: 'solid',
        opacity: 1
      },
      stroke: {
        curve: 'smooth',
        width: 3
      },
      xaxis: {
        categories: dates,
        labels: {
          show: false
        },
        min: minDate,
        max: maxDate
      },
      yaxis: {
        min: minValue,
        max: maxValue,
        labels: {
          show: false
        }
      },
      tooltip: {
        theme: 'dark',
        fixed: {
          enabled: false
        },
        x: {
          format: 'yyyy-MM-dd'
        },
        y: {
          formatter: function (val) {
            return val + ` ${unit}`;
          },
          title: {
            formatter: function () {
              return '';
            }
          }
        },
        marker: {
          show: false
        }
      }
    }
  };
}

export function generateBarChartOptions(xaxis, data, unit = '', decimal = 0) {
  // 记录鼠标当前悬停的堆叠块（seriesIndex），离开块时置空。
  // 用于 tooltip 区分「压在块上」与「在该列空白处」两种交互。
  const hoverState = { seriesIndex: null };
  return {
    height: 480,
    type: 'bar',
    options: {
      title: {
        align: 'left',
        style: {
          fontSize: '14px',
          fontWeight: 'bold',
          fontFamily: 'Roboto, sans-serif'
        }
      },
      colors: [
        '#008FFB',
        '#00E396',
        '#FEB019',
        '#FF4560',
        '#775DD0',
        '#55efc4',
        '#81ecec',
        '#74b9ff',
        '#a29bfe',
        '#00b894',
        '#00cec9',
        '#0984e3',
        '#6c5ce7',
        '#ffeaa7',
        '#fab1a0',
        '#ff7675',
        '#fd79a8',
        '#fdcb6e',
        '#e17055',
        '#d63031',
        '#e84393'
      ],
      chart: {
        id: 'bar-chart',
        stacked: true,
        toolbar: {
          show: true
        },
        zoom: {
          enabled: true
        },
        background: 'transparent',
        events: {
          dataPointMouseEnter: (event, ctx, config) => {
            hoverState.seriesIndex = config.seriesIndex;
          },
          dataPointMouseLeave: () => {
            hoverState.seriesIndex = null;
          }
        }
      },
      responsive: [
        {
          breakpoint: 480,
          options: {
            legend: {
              position: 'bottom',
              offsetX: -10,
              offsetY: 0
            }
          }
        }
      ],
      plotOptions: {
        bar: {
          horizontal: false,
          columnWidth: '50%',
          // borderRadius: 10,
          dataLabels: {
            total: {
              enabled: true,
              style: {
                fontSize: '13px',
                fontWeight: 900
              },
              formatter: function (val) {
                return renderChartNumber(val, decimal);
              }
            }
          }
        }
      },
      xaxis: {
        type: 'category',
        categories: xaxis
      },
      legend: {
        show: true,
        fontSize: '14px',
        fontFamily: `'Roboto', sans-serif`,
        position: 'bottom',
        offsetX: 20,
        labels: {
          useSeriesColors: false
        },
        markers: {
          width: 16,
          height: 16,
          radius: 5
        },
        itemMargin: {
          horizontal: 15,
          vertical: 8
        }
      },
      fill: {
        type: 'solid'
      },
      dataLabels: {
        enabled: false
      },
      grid: {
        show: true
      },
      tooltip: {
        theme: 'dark',
        shared: true,
        intersect: false,
        fixed: {
          enabled: false
        },
        // 压在某块上：显示「总计 + 该块」；在该列空白处：显示「总计 + 该列各块（按值降序取前若干）」。
        custom: function ({ series, dataPointIndex, w }) {
          const format = (val) => renderChartNumber(val, decimal) + (unit ? ` ${unit}` : '');
          const label = w.globals.labels[dataPointIndex];

          let total = 0;
          for (let i = 0; i < series.length; i++) {
            total += series[i][dataPointIndex] || 0;
          }

          const row = (seriesIndex) => {
            const name = w.globals.seriesNames[seriesIndex];
            const value = series[seriesIndex][dataPointIndex] || 0;
            const color = w.globals.colors[seriesIndex];
            return (
              `<div style="display:flex;align-items:center;gap:8px;">` +
              `<span style="width:8px;height:8px;border-radius:50%;background:${color};display:inline-block;"></span>` +
              `<span style="font-size:12px;">${name}</span>` +
              `<span style="font-size:12px;font-weight:600;margin-left:auto;">${format(value)}</span>` +
              `</div>`
            );
          };

          let rows;
          if (hoverState.seriesIndex !== null) {
            // 鼠标精确压在某个块上：只显示该块
            rows = row(hoverState.seriesIndex);
          } else {
            // 在该列空白处：显示该列各块，按值降序，最多 5 条，避免块过多铺满屏幕
            const top = series
              .map((s, i) => ({ i, value: s[dataPointIndex] || 0 }))
              .filter((item) => item.value > 0)
              .sort((a, b) => b.value - a.value)
              .slice(0, 5);
            rows = top.map((item) => row(item.i)).join('');
          }

          return (
            `<div style="padding:8px 12px;">` +
            `<div style="font-size:12px;opacity:0.7;margin-bottom:6px;">${label} · 总计: ${format(total)}</div>` +
            `<div style="display:flex;flex-direction:column;gap:4px;">${rows}</div>` +
            `</div>`
          );
        }
      }
    },
    series: data
  };
}

// 格式化数值
export function renderChartNumber(number, decimal = 2) {
  number = Number(number);

  if (isNaN(number)) {
    return '0';
  }

  if (Math.abs(number) < Number.EPSILON) {
    return '0';
  }

  if (Math.abs(number) >= 1000) {
    return (number / 1000).toFixed(1) + 'k';
  }

  return number.toFixed(decimal);
}
