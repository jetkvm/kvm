import React, { useMemo } from 'react';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';

import { LatencyHistogramData } from '../../hooks/useAudioEvents';

interface LatencyHistogramProps {
  data?: LatencyHistogramData;
  title: string;
  height?: number;
  className?: string;
}

interface ChartDataPoint {
  bucket: string;
  count: number;
  bucketValue: number;
}

const LatencyHistogram: React.FC<LatencyHistogramProps> = ({
  data,
  title,
  height = 200,
  className = ''
}) => {
  // Memoize chart data transformation to avoid recalculation on every render
  const chartData = useMemo((): ChartDataPoint[] => {
    if (!data || !data.buckets || !data.counts || data.buckets.length === 0) {
      return [];
    }

    const transformedData: ChartDataPoint[] = [];
    
    // Process each bucket with its count
    for (let i = 0; i < data.buckets.length; i++) {
      const bucketValue = data.buckets[i];
      const count = data.counts[i] || 0;
      
      // Skip empty buckets to reduce chart clutter
      if (count === 0) continue;
      
      // Format bucket label based on value
      let bucketLabel: string;
      if (bucketValue < 1) {
        bucketLabel = `${(bucketValue * 1000).toFixed(0)}μs`;
      } else if (bucketValue < 1000) {
        bucketLabel = `${bucketValue.toFixed(1)}ms`;
      } else {
        bucketLabel = `${(bucketValue / 1000).toFixed(1)}s`;
      }
      
      transformedData.push({
        bucket: bucketLabel,
        count,
        bucketValue
      });
    }
    
    // Handle overflow bucket (last count if it exists)
    if (data.counts.length > data.buckets.length) {
      const overflowCount = data.counts[data.counts.length - 1];
      if (overflowCount > 0) {
        transformedData.push({
          bucket: '>2s',
          count: overflowCount,
          bucketValue: 2000 // 2 seconds in ms
        });
      }
    }
    
    return transformedData;
  }, [data]);

  // Custom tooltip for better UX
  const CustomTooltip = ({ active, payload, label }: {
    active?: boolean;
    payload?: { payload: ChartDataPoint }[];
    label?: string;
  }) => {
    if (active && payload && payload.length) {
      const data = payload[0].payload;
      return (
        <div className="bg-gray-800 text-white p-2 rounded shadow-lg border border-gray-600">
          <p className="font-medium">{`Latency: ${label}`}</p>
          <p className="text-blue-300">{`Count: ${data.count}`}</p>
        </div>
      );
    }
    return null;
  };

  if (!data || chartData.length === 0) {
    return (
      <div className={`bg-gray-50 dark:bg-gray-800 rounded-lg p-4 ${className}`}>
        <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
          {title}
        </h3>
        <div className="flex items-center justify-center h-32 text-gray-500 dark:text-gray-400">
          <span className="text-sm">No latency data available</span>
        </div>
      </div>
    );
  }

  return (
    <div className={`bg-gray-50 dark:bg-gray-800 rounded-lg p-4 ${className}`}>
      <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        {title}
      </h3>
      <ResponsiveContainer width="100%" height={height}>
        <BarChart
          data={chartData}
          margin={{
            top: 5,
            right: 5,
            left: 5,
            bottom: 5,
          }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke="#374151" opacity={0.3} />
          <XAxis 
            dataKey="bucket" 
            tick={{ fontSize: 11, fill: '#6B7280' }}
            axisLine={{ stroke: '#6B7280' }}
            tickLine={{ stroke: '#6B7280' }}
          />
          <YAxis 
            tick={{ fontSize: 11, fill: '#6B7280' }}
            axisLine={{ stroke: '#6B7280' }}
            tickLine={{ stroke: '#6B7280' }}
          />
          <Tooltip content={<CustomTooltip />} />
          <Bar 
            dataKey="count" 
            fill="#3B82F6" 
            radius={[2, 2, 0, 0]}
            stroke="#1E40AF"
            strokeWidth={1}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
};

export default LatencyHistogram;